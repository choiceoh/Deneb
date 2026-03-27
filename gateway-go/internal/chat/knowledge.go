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
		wg              sync.WaitGroup
		vegaResults     []vega.SearchResult
		memMatches      []MemoryMatch
		structFacts     []memory.SearchResult
		userModelEntries []memory.UserModelEntry
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

		// Fetch mutual understanding / user model (parallel).
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries, err := deps.MemoryStore.GetUserModel(ctx)
			if err == nil {
				userModelEntries = entries
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

	// Build the combined knowledge section.
	var parts []string

	// Knowledge section (Vega + memory facts).
	if len(vegaResults) > 0 || len(memMatches) > 0 || len(structFacts) > 0 {
		parts = append(parts, formatKnowledgeWithFacts(vegaResults, memMatches, structFacts))
	}

	// Mutual understanding section (user model).
	if mu := formatMutualUnderstanding(userModelEntries); mu != "" {
		parts = append(parts, mu)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
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

// userProfileKeys defines display order and Korean labels for Phase 5 user model keys.
var userProfileKeys = []struct {
	Key   string
	Label string
}{
	{"communication_style", "소통 스타일"},
	{"expertise_areas", "전문 영역"},
	{"tech_preferences", "기술 선호"},
	{"common_tasks", "주요 작업"},
	{"work_patterns", "작업 패턴"},
}

// mutualUnderstandingKeys defines display order and Korean labels for Phase 6 mutual keys.
var mutualUnderstandingKeys = []struct {
	Key   string
	Label string
}{
	{"user_sees_ai", "사용자 → AI 인식"},
	{"ai_understands_user", "AI → 사용자 이해"},
	{"relationship_dynamics", "관계 역학"},
	{"adaptation_notes", "적응 메모"},
}

// mutualUnderstandingMaxTokens caps the entire mutual understanding + user profile section.
const mutualUnderstandingMaxTokens = 1500

// formatMutualUnderstanding builds the "## 상호 인식" section from user_model entries.
// Includes both the user profile (Phase 5) and relationship dynamics (Phase 6),
// plus behavioral guidance so the AI knows HOW to use this information.
func formatMutualUnderstanding(entries []memory.UserModelEntry) string {
	if len(entries) == 0 {
		return ""
	}

	// Index entries by key for fast lookup.
	byKey := make(map[string]memory.UserModelEntry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	var sb strings.Builder
	tokenCount := 0
	hasContent := false

	// Section 1: User profile (Phase 5 data).
	profileContent := formatKeySection(byKey, userProfileKeys)
	if profileContent != "" {
		sb.WriteString("## 상호 인식\n\n")
		sb.WriteString("### 사용자 프로필\n")
		sb.WriteString(profileContent)
		sb.WriteString("\n")
		tokenCount += sb.Len() / charsPerToken
		hasContent = true
	}

	// Section 2: Relationship dynamics (Phase 6 data).
	for _, mk := range mutualUnderstandingKeys {
		if tokenCount >= mutualUnderstandingMaxTokens {
			break
		}
		e, ok := byKey[mk.Key]
		if !ok || e.Value == "" {
			continue
		}

		if !hasContent {
			sb.WriteString("## 상호 인식\n\n")
			hasContent = true
		}

		before := sb.Len()
		content := truncateRunes(e.Value, knowledgeMaxContentRunes)
		fmt.Fprintf(&sb, "### %s\n%s\n\n", mk.Label, content)
		tokenCount += (sb.Len() - before) / charsPerToken
	}

	if !hasContent {
		return ""
	}

	// Behavioral guidance: tell the AI HOW to use this information.
	if tokenCount < mutualUnderstandingMaxTokens {
		sb.WriteString("### 활용 지침\n")
		sb.WriteString("위 상호 인식은 대화를 통해 축적된 이해입니다. 이를 바탕으로:\n")
		sb.WriteString("- 적응 메모의 지시사항을 즉시 적용하세요\n")
		sb.WriteString("- 사용자 프로필에 맞게 답변 스타일을 조절하세요\n")
		sb.WriteString("- 관계 역학을 고려해 톤과 상세도를 맞추세요\n")
		sb.WriteString("- 이 정보를 사용자에게 직접 언급하지 마세요 (자연스럽게 반영만)\n\n")
	}

	return sb.String()
}

// formatKeySection formats a set of user_model keys into "- Label: value" lines.
func formatKeySection(byKey map[string]memory.UserModelEntry, keys []struct{ Key, Label string }) string {
	var sb strings.Builder
	for _, k := range keys {
		e, ok := byKey[k.Key]
		if !ok || e.Value == "" {
			continue
		}
		content := truncateRunes(e.Value, knowledgeMaxContentRunes)
		fmt.Fprintf(&sb, "- **%s**: %s\n", k.Label, content)
	}
	return sb.String()
}
