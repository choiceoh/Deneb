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
	"github.com/choiceoh/deneb/gateway-go/internal/unified"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// KnowledgeDeps holds optional dependencies for knowledge prefetch.
type KnowledgeDeps struct {
	VegaBackend    vega.Backend     // nil → skip Vega search
	WorkspaceDir   string           // empty → skip file-based Memory search
	MemoryStore    *memory.Store    // nil → skip structured memory search
	MemoryEmbedder *memory.Embedder // nil → FTS-only structured search
	UnifiedStore   *unified.Store   // nil → skip unified search + tier-1 injection
}

// Knowledge prefetch limits.
const (
	knowledgeMaxTokens       = 5000 // ~20KB of text budget
	knowledgeMaxVega         = 5    // top Vega results
	knowledgeMaxMemory       = 10   // top memory matches (token budget is the real ceiling)
	knowledgeTimeout         = 5 * time.Second
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
	// Early return when no knowledge sources are configured (common for Telegram
	// chat profiles without Vega or memory). Avoids WaitGroup + goroutine overhead.
	if deps.VegaBackend == nil && deps.MemoryStore == nil && deps.WorkspaceDir == "" && deps.UnifiedStore == nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, knowledgeTimeout)
	defer cancel()

	var (
		wg               sync.WaitGroup
		vegaResults      []vega.SearchResult
		memMatches       []MemoryMatch
		structFacts      []memory.SearchResult
		unifiedResults   []unified.SearchResult
		userModelEntries []memory.UserModelEntry
		tier1Section     string // high-importance facts always-injected
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
			searchOpts := memory.SearchOpts{Limit: knowledgeMaxMemory}
			if deps.MemoryEmbedder == nil {
				// No semantic search available: restrict FTS scan to high-importance facts
				// so hundreds of low-signal facts don't get scanned on every message.
				searchOpts.MinImportance = 0.7
			}
			results, err := deps.MemoryStore.SearchFacts(ctx, message, queryVec, searchOpts)
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

	// Tier 1: always-inject high-importance facts (unified store).
	if deps.UnifiedStore != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := deps.UnifiedStore.Search(ctx, message, unified.SearchOpts{Limit: knowledgeMaxMemory})
			if err == nil {
				unifiedResults = filterUnifiedKnowledgeResults(message, results, deps.MemoryStore == nil)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			tier1Section = deps.UnifiedStore.Tier1Section(ctx)
		}()
	}

	wg.Wait()

	// Build the combined knowledge section.
	var parts []string

	// Tier 1: high-importance facts always at the top.
	if tier1Section != "" {
		parts = append(parts, tier1Section)
	}

	// Knowledge section (Vega + memory facts).
	if len(vegaResults) > 0 || len(memMatches) > 0 || len(structFacts) > 0 || len(unifiedResults) > 0 {
		parts = append(parts, formatKnowledgeWithFacts(vegaResults, memMatches, structFacts, unifiedResults))
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

// categoryVolatileDays defines "shelf life" per fact category.
// Facts older than this threshold (relative to UpdatedAt) get a staleness hint.
// Stable facts (names, preferences) have long shelf lives; volatile facts (context, decisions)
// go stale faster.
var categoryVolatileDays = map[string]int{
	"context":    30,  // project context changes frequently
	"decision":   60,  // decisions may be revisited
	"solution":   90,  // solutions stay relevant longer
	"preference": 365, // preferences are relatively stable
	"user_model": 365, // user traits rarely change
	"mutual":     180, // relationship dynamics evolve slowly
}

// volatileHint returns a staleness hint based on how far past the category shelf life:
//   - past 50% of shelf life → "확인 필요" (should verify)
//   - past 100% of shelf life → "⚠변경 가능" (likely stale)
//   - within 50% → "" (still fresh)
func volatileHint(category string, updatedAt time.Time, now time.Time) string {
	if updatedAt.IsZero() {
		return ""
	}
	shelfDays, ok := categoryVolatileDays[category]
	if !ok {
		shelfDays = 60 // conservative default
	}
	age := now.Sub(updatedAt)
	shelf := time.Duration(shelfDays) * 24 * time.Hour
	switch {
	case age > shelf:
		return "⚠변경 가능"
	case age > shelf/2:
		return "확인 필요"
	default:
		return ""
	}
}

// relativeTimeSince returns a concise Korean relative time label for t relative to now.
// Returns "" for zero time. Used to give the LLM temporal context for memory facts.
func relativeTimeSince(t time.Time, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	if d < 0 {
		return "방금"
	}
	switch {
	case d < time.Hour:
		return "방금"
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%d시간 전", h)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		switch days {
		case 1:
			return "어제"
		case 2:
			return "그저께"
		default:
			return fmt.Sprintf("%d일 전", days)
		}
	case d < 30*24*time.Hour:
		weeks := int(d.Hours() / 24 / 7)
		return fmt.Sprintf("%d주 전", weeks)
	case d < 365*24*time.Hour:
		months := int(d.Hours() / 24 / 30)
		if months < 1 {
			months = 1
		}
		return fmt.Sprintf("%d개월 전", months)
	default:
		years := int(d.Hours() / 24 / 365)
		if years < 1 {
			years = 1
		}
		return fmt.Sprintf("%d년 전", years)
	}
}

// factTemporalAnnotation builds a compact temporal label for a memory fact.
// Combines relative time, CreatedAt/UpdatedAt separation, and volatility hints.
// Examples:
//   - "3일 전" — simple case (created == updated or small gap)
//   - "3개월 전, 갱신: 어제" — created long ago but recently re-confirmed
//   - "6개월 전, ⚠변경 가능" — past shelf life for its category
func factTemporalAnnotation(f memory.Fact, now time.Time) string {
	hasCreated := !f.CreatedAt.IsZero()
	hasUpdated := !f.UpdatedAt.IsZero()

	if !hasCreated && !hasUpdated {
		return ""
	}

	var parts []string

	// Show CreatedAt/UpdatedAt separately when both exist and differ significantly (>7 days).
	// "3개월 전, 갱신: 어제" = long-known, recently confirmed.
	// "3일 전" = recently created, no meaningful gap.
	if hasCreated && hasUpdated && f.UpdatedAt.Sub(f.CreatedAt) > 7*24*time.Hour {
		parts = append(parts,
			relativeTimeSince(f.CreatedAt, now)+", 갱신: "+relativeTimeSince(f.UpdatedAt, now))
	} else if hasUpdated {
		parts = append(parts, relativeTimeSince(f.UpdatedAt, now))
	} else {
		parts = append(parts, relativeTimeSince(f.CreatedAt, now))
	}

	// Volatility hint: use UpdatedAt (or CreatedAt fallback) to check shelf life.
	refTime := f.UpdatedAt
	if !hasUpdated {
		refTime = f.CreatedAt
	}
	if hint := volatileHint(f.Category, refTime, now); hint != "" {
		parts = append(parts, hint)
	}

	return strings.Join(parts, ", ")
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
	return formatKnowledgeWithFacts(vegaResults, memMatches, nil, nil)
}

// formatKnowledgeWithFacts builds the "## 관련 지식" section from search results,
// respecting the token budget. Supports both legacy MemoryMatch and structured facts.
func formatKnowledgeWithFacts(
	vegaResults []vega.SearchResult,
	memMatches []MemoryMatch,
	structFacts []memory.SearchResult,
	unifiedResults []unified.SearchResult,
) string {
	now := time.Now() // capture once for consistent temporal annotations across all facts
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
			// Temporal annotation with three layers:
			// 1. Relative time (how old)
			// 2. CreatedAt/UpdatedAt separation (when significantly different)
			// 3. Volatility hint (past shelf life for this category)
			timeAnnotation := factTemporalAnnotation(sr.Fact, now)
			if timeAnnotation != "" {
				fmt.Fprintf(&sb, "- [%.1f] {%s} (%s) %s\n", sr.Fact.Importance, sr.Fact.Category, timeAnnotation, content)
			} else {
				fmt.Fprintf(&sb, "- [%.1f] {%s} %s\n", sr.Fact.Importance, sr.Fact.Category, content)
			}
			tokenCount += (sb.Len() - before) / charsPerToken

			if tokenCount >= knowledgeMaxTokens {
				break
			}
		}
	}

	// Unified recall (messages/summaries, plus fact fallback when structured search is absent).
	if len(unifiedResults) > 0 && tokenCount < knowledgeMaxTokens {
		before := sb.Len()
		sb.WriteString("### 대화 기억\n")
		tokenCount += (sb.Len() - before) / charsPerToken

		for _, r := range unifiedResults {
			before = sb.Len()
			content := truncateRunes(r.Content, knowledgeMaxContentRunes)
			label := unifiedItemLabel(r.ItemType)
			if r.CreatedAt > 0 {
				fmt.Fprintf(&sb, "- {%s} (%s) %s\n", label, relativeTimeSince(time.UnixMilli(r.CreatedAt), now), content)
			} else {
				fmt.Fprintf(&sb, "- {%s} %s\n", label, content)
			}
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

func filterUnifiedKnowledgeResults(query string, results []unified.SearchResult, includeFacts bool) []unified.SearchResult {
	normalizedQuery := normalizeKnowledgeRecallText(query)
	filtered := make([]unified.SearchResult, 0, len(results))
	for _, r := range results {
		if r.Content == "" {
			continue
		}
		if !includeFacts && r.ItemType == "fact" {
			continue
		}
		if normalizedQuery != "" && r.ItemType == "message" && normalizeKnowledgeRecallText(r.Content) == normalizedQuery {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

func normalizeKnowledgeRecallText(s string) string {
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func unifiedItemLabel(itemType string) string {
	switch itemType {
	case "summary":
		return "요약"
	case "fact":
		return "기억"
	default:
		return "메시지"
	}
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

	// Section 3: Recent unprocessed signals (real-time, between dreaming cycles).
	// These haven't been consolidated yet but give the AI immediate awareness
	// of recent relationship dynamics so it can adapt mid-session.
	if tokenCount < mutualUnderstandingMaxTokens {
		if raw, ok := byKey["mu_signals_raw"]; ok && raw.Value != "" {
			lines := strings.Split(strings.TrimSpace(raw.Value), "\n")
			// Show only the most recent signals to keep it concise.
			maxRecent := 5
			if len(lines) > maxRecent {
				lines = lines[len(lines)-maxRecent:]
			}
			before := sb.Len()
			sb.WriteString("### 최근 시그널 (미통합)\n")
			for _, line := range lines {
				if line = strings.TrimSpace(line); line != "" {
					fmt.Fprintf(&sb, "- %s\n", truncateRunes(line, 200))
				}
			}
			sb.WriteString("\n")
			tokenCount += (sb.Len() - before) / charsPerToken
		}
	}

	// Section 4: Relationship history snapshot (multi-cycle evolution).
	if tokenCount < mutualUnderstandingMaxTokens {
		if hist, ok := byKey["mu_history"]; ok && hist.Value != "" {
			before := sb.Len()
			sb.WriteString("### 관계 변화 이력\n")
			sb.WriteString(truncateRunes(hist.Value, 300))
			sb.WriteString("\n\n")
			tokenCount += (sb.Len() - before) / charsPerToken
		}
	}

	// Section 5: Behavioral guidance with priority framework.
	if tokenCount < mutualUnderstandingMaxTokens {
		sb.WriteString("### 활용 지침\n")
		sb.WriteString("위 상호 인식은 대화를 통해 축적된 이해입니다. 적용 우선순위:\n")
		sb.WriteString("1. **적응 메모** (최우선): 구체적 행동 지시사항을 즉시 적용\n")
		sb.WriteString("2. **최근 시그널** (높음): 아직 통합되지 않은 최신 피드백 — 적응 메모보다 최신이면 이쪽 우선\n")
		sb.WriteString("3. **사용자 프로필** (중간): 답변 길이, 톤, 상세도를 프로필에 맞춤\n")
		sb.WriteString("4. **관계 역학** (배경): 전반적 관계 톤과 신뢰 수준을 고려\n\n")
		sb.WriteString("충돌 시: 최신 시그널 > 적응 메모 > 프로필 > 역학 (더 최근의 정보가 우선)\n")
		sb.WriteString("이 정보를 사용자에게 직접 언급하지 마세요 — 자연스럽게 반영만 하세요.\n\n")
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
