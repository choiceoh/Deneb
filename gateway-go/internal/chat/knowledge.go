// Knowledge prefetch: enriches system prompt with relevant project knowledge
// and memory matches before the LLM sees the conversation.
//
// Runs Vega (project DB) and Memory (markdown files) searches in parallel,
// then formats results as a "## 관련 지식" section appended to the system prompt.
package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// KnowledgeDeps holds optional dependencies for knowledge prefetch.
type KnowledgeDeps struct {
	VegaBackend  vega.Backend // nil → skip Vega search
	WorkspaceDir string       // empty → skip Memory search
}

// Knowledge prefetch limits.
const (
	knowledgeMaxTokens     = 5000 // ~20KB of text budget
	knowledgeMaxVega       = 5    // top Vega results
	knowledgeMaxMemory     = 5    // top memory matches
	knowledgeTimeout       = 3 * time.Second
	knowledgeMaxContentRunes = 500 // truncate individual result content (in runes, not bytes)
)

// PrefetchKnowledge searches Vega and Memory in parallel for content relevant
// to the user message. Returns a formatted section to append to the system
// prompt, or "" if nothing relevant was found.
// minPrefetchRunes is the minimum message length to trigger knowledge prefetch.
// Skips very short messages (greetings, reactions) that are unlikely to benefit.
const minPrefetchRunes = 4

func PrefetchKnowledge(ctx context.Context, message string, deps KnowledgeDeps) string {
	if utf8.RuneCountInString(message) < minPrefetchRunes {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, knowledgeTimeout)
	defer cancel()

	var (
		wg          sync.WaitGroup
		vegaResults []vega.SearchResult
		memMatches  []MemoryMatch
	)

	// Vega search (project knowledge DB).
	if deps.VegaBackend != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := deps.VegaBackend.Search(ctx, message, vega.SearchOpts{Limit: knowledgeMaxVega})
			if err == nil {
				vegaResults = results
			}
		}()
	}

	// Memory search (MEMORY.md + memory/*.md).
	if deps.WorkspaceDir != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			memMatches = searchMemoryFiles(deps.WorkspaceDir, message, knowledgeMaxMemory)
		}()
	}

	wg.Wait()

	if len(vegaResults) == 0 && len(memMatches) == 0 {
		return ""
	}

	return formatKnowledge(vegaResults, memMatches)
}

// truncateRunes truncates s to at most maxRunes runes, appending "..." if truncated.
// Safe for multibyte UTF-8 (Korean, etc.).
func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "..."
}

// formatKnowledge builds the "## 관련 지식" section from search results,
// respecting the token budget.
func formatKnowledge(vegaResults []vega.SearchResult, memMatches []MemoryMatch) string {
	var sb strings.Builder
	sb.WriteString("## 관련 지식\n\n")

	// Vega project results.
	for _, r := range vegaResults {
		content := truncateRunes(r.Content, knowledgeMaxContentRunes)
		fmt.Fprintf(&sb, "### 프로젝트: %s\n", r.ProjectName)
		if r.Section != "" {
			fmt.Fprintf(&sb, "**%s**: %s\n\n", r.Section, content)
		} else {
			fmt.Fprintf(&sb, "%s\n\n", content)
		}

		if estimateTokens(sb.String()) >= knowledgeMaxTokens {
			break
		}
	}

	// Memory matches.
	if len(memMatches) > 0 && estimateTokens(sb.String()) < knowledgeMaxTokens {
		sb.WriteString("### 메모리\n")
		for _, m := range memMatches {
			snippet := truncateRunes(m.Snippet, knowledgeMaxContentRunes)
			fmt.Fprintf(&sb, "- %s (line %d): %s\n", m.File, m.Line, snippet)

			if estimateTokens(sb.String()) >= knowledgeMaxTokens {
				break
			}
		}
	}

	result := sb.String()
	if estimateTokens(result) < 10 {
		return "" // too little content to be useful
	}
	return result
}
