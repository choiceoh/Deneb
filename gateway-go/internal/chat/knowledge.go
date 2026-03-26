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

	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// KnowledgeDeps holds optional dependencies for knowledge prefetch.
type KnowledgeDeps struct {
	VegaBackend    vega.Backend     // nil → skip Vega search
	WorkspaceDir   string           // empty → skip file-based Memory search
	MemoryStore    *memory.Store    // nil → skip structured memory search
	MemoryEmbedder *memory.Embedder // nil → FTS-only structured search
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
const minPrefetchRunes = 2

func PrefetchKnowledge(ctx context.Context, message string, deps KnowledgeDeps) string {
	if utf8.RuneCountInString(message) < minPrefetchRunes {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, knowledgeTimeout)
	defer cancel()

	var (
		wg           sync.WaitGroup
		vegaResults  []vega.SearchResult
		memMatches   []MemoryMatch
		structFacts  []memory.SearchResult
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

	// Structured memory search (Honcho-style SQLite store).
	if deps.MemoryStore != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Optionally embed query for semantic search.
			var queryVec []float32
			if deps.MemoryEmbedder != nil {
				vec, err := deps.MemoryEmbedder.EmbedQuery(ctx, message)
				if err == nil {
					queryVec = vec
				}
			}
			results, err := deps.MemoryStore.SearchFacts(ctx, message, queryVec, memory.SearchOpts{Limit: knowledgeMaxMemory})
			if err == nil {
				structFacts = results
			}
		}()
	} else if deps.WorkspaceDir != "" {
		// Fallback: file-based memory search (legacy).
		wg.Add(1)
		go func() {
			defer wg.Done()
			memMatches = searchMemoryFiles(deps.WorkspaceDir, message, knowledgeMaxMemory)
		}()
	}

	wg.Wait()

	if len(vegaResults) == 0 && len(memMatches) == 0 && len(structFacts) == 0 {
		return ""
	}

	return formatKnowledgeWithFacts(vegaResults, memMatches, structFacts)
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

// formatKnowledge builds the "## 관련 지식" section from search results (legacy).
func formatKnowledge(vegaResults []vega.SearchResult, memMatches []MemoryMatch) string {
	return formatKnowledgeWithFacts(vegaResults, memMatches, nil)
}

// formatKnowledgeWithFacts builds the "## 관련 지식" section from search results,
// respecting the token budget. Supports both legacy MemoryMatch and structured facts.
func formatKnowledgeWithFacts(vegaResults []vega.SearchResult, memMatches []MemoryMatch, structFacts []memory.SearchResult) string {
	var sb strings.Builder
	sb.WriteString("## 관련 지식\n\n")
	tokenCount := sb.Len() / charsPerToken

	// Vega project results.
	for _, r := range vegaResults {
		before := sb.Len()
		content := truncateRunes(r.Content, knowledgeMaxContentRunes)
		fmt.Fprintf(&sb, "### 프로젝트: %s\n", r.ProjectName)
		if r.Section != "" {
			fmt.Fprintf(&sb, "**%s**: %s\n\n", r.Section, content)
		} else {
			fmt.Fprintf(&sb, "%s\n\n", content)
		}
		tokenCount += (sb.Len() - before) / charsPerToken

		if tokenCount >= knowledgeMaxTokens {
			break
		}
	}

	// Structured memory facts (Honcho-style, importance-weighted).
	if len(structFacts) > 0 && tokenCount < knowledgeMaxTokens {
		before := sb.Len()
		sb.WriteString("### 메모리\n")
		tokenCount += (sb.Len() - before) / charsPerToken

		for _, sr := range structFacts {
			before = sb.Len()
			content := truncateRunes(sr.Fact.Content, knowledgeMaxContentRunes)
			fmt.Fprintf(&sb, "- [%.1f] {%s} %s\n", sr.Fact.Importance, sr.Fact.Category, content)
			tokenCount += (sb.Len() - before) / charsPerToken

			if tokenCount >= knowledgeMaxTokens {
				break
			}
		}
	}

	// Legacy memory matches (file-based fallback).
	if len(memMatches) > 0 && len(structFacts) == 0 && tokenCount < knowledgeMaxTokens {
		before := sb.Len()
		sb.WriteString("### 메모리\n")
		tokenCount += (sb.Len() - before) / charsPerToken

		for _, m := range memMatches {
			before = sb.Len()
			snippet := truncateRunes(m.Snippet, knowledgeMaxContentRunes)
			fmt.Fprintf(&sb, "- %s (line %d): %s\n", m.File, m.Line, snippet)
			tokenCount += (sb.Len() - before) / charsPerToken

			if tokenCount >= knowledgeMaxTokens {
				break
			}
		}
	}

	if tokenCount < 10 {
		return "" // too little content to be useful
	}
	return sb.String()
}
