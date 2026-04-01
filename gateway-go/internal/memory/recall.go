// recall.go — Dedicated LLM-based memory recall with relation chain traversal,
// entity expansion, and lazy backfill of missing entity/relation data.
//
// The recall engine runs as a pilot LLM call in parallel with the main LLM,
// producing a rich context pack of relevant facts, entity summaries, and
// a timeline. Falls back to standard SearchFacts when the pilot is unavailable.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// RecallConfig controls the recall engine behavior.
type RecallConfig struct {
	Enabled  bool          // enable pilot-based recall (default true)
	Timeout  time.Duration // recall LLM timeout (default 5s)
	MaxFacts int           // max facts in context pack (default 20)
	MaxDepth int           // max relation chain depth (default 3)
}

// DefaultRecallConfig returns sensible defaults for single-user DGX Spark.
func DefaultRecallConfig() RecallConfig {
	return RecallConfig{
		Enabled:  true,
		Timeout:  5 * time.Second,
		MaxFacts: 20,
		MaxDepth: 3,
	}
}

// RecallResult is the context pack produced by the recall engine.
type RecallResult struct {
	Facts          []RecalledFact        `json:"relevant_facts"`
	EntitySummary  map[string]string     `json:"entity_summaries,omitempty"`
	Timeline       string                `json:"timeline,omitempty"`
	Backfill       *BackfillData         `json:"backfill,omitempty"`
}

// RecalledFact is a fact selected by the recall engine with a reason.
type RecalledFact struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// BackfillData contains entity/relation extractions for facts that were
// missing this data. Processed asynchronously after recall completes.
type BackfillData struct {
	Entities  []BackfillEntity  `json:"entities,omitempty"`
	Relations []BackfillRelation `json:"relations,omitempty"`
}

// BackfillEntity is an entity extracted during backfill.
type BackfillEntity struct {
	FactID int64  `json:"fact_id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Role   string `json:"role"`
}

// BackfillRelation is a relation extracted during backfill.
type BackfillRelation struct {
	FromID     int64   `json:"from_id"`
	ToID       int64   `json:"to_id"`
	Type       string  `json:"type"`
	Confidence float64 `json:"confidence"`
}

// recallSystemPrompt is the system prompt for the recall pilot LLM.
const recallSystemPrompt = `당신은 메모리 recall 전용 어시스턴트입니다. 오직 메모리 검색과 컨텍스트 구성에만 집중하세요.

규칙:
1. 빠짐없이 (exhaustive): 관련성이 조금이라도 있으면 포함. 과잉보다 누락이 나쁩니다.
2. 관계를 따라가세요: hits된 팩트의 evolves/contradicts/supports/causes 관계를 쫓아서 연관 팩트를 확장하세요.
3. 엔티티로 확장하세요: 질문에 언급된 객체와 관련된 모든 엔티티의 팩트를 확인하세요.
4. 최신 것이 우선: 같은 주제라면 최근 팩트를 우선 포함하세요.
5. 모순을 포함하세요: 과거와 현재가 다르면 둘 다 포함. "과거에는 X였으나 지금은 Y로 변경"이 유용한 정보입니다.

제외 기준 (anti-noise):
- 만료된 팩트 (expired)
- importance 0.3 미만 팩트

backfill: 아래 "빈 팩트" 목록에 해당하는 팩트들은 엔티티/관계 정보가 비어있습니다. 이 팩트들을 읽고 엔티티와 팩트 간 관계를 추출해서 backfill 필드에 반환하세요.

출력 형식 (JSON):
{
  "relevant_facts": [
    {"id": 42, "reason": "직접 관련: SQLite 선호 패턴"},
    {"id": 15, "reason": "인과: 이 결정으로 이어진 논의"}
  ],
  "entity_summaries": {
    "SGLang": "최근 7개 팩트에서 언급. 주제: 설정, 성능, 재시작."
  },
  "timeline": "2026-03-15 DB 고민 → 03-18 SQLite 선호 → 03-30 migration",
  "backfill": {
    "entities": [{"fact_id": 42, "name": "SGLang", "type": "tool", "role": "subject"}],
    "relations": [{"from_id": 42, "to_id": 15, "type": "supports", "confidence": 0.7}]
  }
}

Return ONLY valid JSON, no markdown fences, no explanation.`

// Recall performs a pilot LLM-based memory recall for the given user message.
// It searches facts, expands via entities and relation chains, then asks the
// pilot LLM to select and organize the most relevant facts.
//
// Returns formatted knowledge text ready for system prompt injection, or ""
// if recall produces no results. Falls back to standard search on any error.
func Recall(ctx context.Context, store *Store, embedder *Embedder, client *llm.Client, model string, message string, cfg RecallConfig, logger *slog.Logger) string {
	if !cfg.Enabled || client == nil || store == nil {
		return ""
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	maxFacts := cfg.MaxFacts
	if maxFacts <= 0 {
		maxFacts = 20
	}

	// Phase 1: Gather candidate facts via standard search.
	var queryVec []float32
	if embedder != nil {
		if vec, err := embedder.EmbedQuery(ctx, message); err == nil {
			queryVec = vec
		}
	}
	candidates, err := store.SearchFacts(ctx, message, queryVec, SearchOpts{
		Limit: maxFacts,
	})
	if err != nil || len(candidates) == 0 {
		return ""
	}

	// Phase 2: Expand via entity matching.
	entityFacts := expandViaEntities(ctx, store, candidates, maxFacts)
	candidates = mergeSearchResults(candidates, entityFacts)

	// Phase 3: Expand via relation chains.
	maxDepth := cfg.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 3
	}
	relationFacts := expandViaRelations(ctx, store, candidates, maxDepth)
	candidates = mergeSearchResults(candidates, relationFacts)

	// Truncate to maxFacts.
	if len(candidates) > maxFacts {
		candidates = candidates[:maxFacts]
	}

	// Phase 4: Identify facts missing entity/relation data for backfill.
	backfillIDs := findBackfillCandidates(ctx, store, candidates, 20)

	// Phase 5: Build prompt and call pilot LLM.
	userPrompt := buildRecallPrompt(message, candidates, backfillIDs)
	result, err := callLLMJSON[RecallResult](ctx, client, model, recallSystemPrompt, userPrompt, 2048)
	if err != nil {
		logger.Debug("recall: pilot LLM failed, using raw search results", "error", err)
		return formatCandidatesAsKnowledge(candidates)
	}

	// Phase 6: Process backfill asynchronously (fire-and-forget).
	if result.Backfill != nil && (len(result.Backfill.Entities) > 0 || len(result.Backfill.Relations) > 0) {
		go processBackfill(store, result.Backfill, logger)
	}

	// Phase 7: Format recall result as knowledge text.
	return formatRecallResult(&result, candidates)
}

// expandViaEntities finds entities linked to existing candidates and adds
// their related facts to the candidate pool.
func expandViaEntities(ctx context.Context, store *Store, existing []SearchResult, maxFacts int) []SearchResult {
	// Extract entity names from existing candidates.
	entityNames := make(map[string]bool)
	for _, sr := range existing {
		names := store.getFactEntityNames(ctx, sr.Fact.ID)
		for _, name := range names {
			entityNames[name] = true
		}
	}

	var expanded []SearchResult
	existingIDs := make(map[int64]bool, len(existing))
	for _, sr := range existing {
		existingIDs[sr.Fact.ID] = true
	}

	for name := range entityNames {
		facts, err := store.GetFactsByEntity(ctx, name)
		if err != nil {
			continue
		}
		for _, f := range facts {
			if existingIDs[f.ID] || !f.Active {
				continue
			}
			existingIDs[f.ID] = true
			expanded = append(expanded, SearchResult{
				Fact:  f,
				Score: 0.5, // baseline score for entity expansion
			})
			if len(expanded) >= maxFacts/2 { // cap entity expansion
				return expanded
			}
		}
	}
	return expanded
}

// expandViaRelations follows relation chains from candidate facts.
// Caps total expansion to avoid N+1 query explosion under tight timeout.
const maxRelationExpansion = 20

func expandViaRelations(ctx context.Context, store *Store, candidates []SearchResult, maxDepth int) []SearchResult {
	existingIDs := make(map[int64]bool, len(candidates))
	for _, sr := range candidates {
		existingIDs[sr.Fact.ID] = true
	}

	var expanded []SearchResult
	for _, sr := range candidates {
		if len(expanded) >= maxRelationExpansion {
			break
		}

		related, err := store.GetRelatedFacts(ctx, sr.Fact.ID)
		if err != nil {
			continue
		}
		for _, rf := range related {
			if existingIDs[rf.Fact.ID] || !rf.Fact.Active {
				continue
			}
			existingIDs[rf.Fact.ID] = true
			expanded = append(expanded, SearchResult{
				Fact:  rf.Fact,
				Score: 0.4, // baseline for relation expansion
			})
			if len(expanded) >= maxRelationExpansion {
				return expanded
			}
		}

		// Follow evolves/supports chains deeper.
		for _, relType := range []string{RelationEvolves, RelationSupports} {
			if len(expanded) >= maxRelationExpansion {
				return expanded
			}
			chain, err := store.GetRelationChain(ctx, sr.Fact.ID, relType, maxDepth)
			if err != nil {
				continue
			}
			for _, f := range chain {
				if existingIDs[f.ID] || !f.Active {
					continue
				}
				existingIDs[f.ID] = true
				expanded = append(expanded, SearchResult{
					Fact:  f,
					Score: 0.35,
				})
				if len(expanded) >= maxRelationExpansion {
					return expanded
				}
			}
		}
	}
	return expanded
}

// mergeSearchResults appends additional results to existing, avoiding duplicates.
func mergeSearchResults(existing, additional []SearchResult) []SearchResult {
	ids := make(map[int64]bool, len(existing))
	for _, sr := range existing {
		ids[sr.Fact.ID] = true
	}
	for _, sr := range additional {
		if !ids[sr.Fact.ID] {
			existing = append(existing, sr)
			ids[sr.Fact.ID] = true
		}
	}
	return existing
}

// findBackfillCandidates identifies facts in the candidate pool that have
// no entity links (fact_entities is empty). Returns up to maxBackfill IDs.
func findBackfillCandidates(ctx context.Context, store *Store, candidates []SearchResult, maxBackfill int) []int64 {
	var ids []int64
	for _, sr := range candidates {
		if len(ids) >= maxBackfill {
			break
		}
		names := store.getFactEntityNames(ctx, sr.Fact.ID)
		if len(names) == 0 {
			ids = append(ids, sr.Fact.ID)
		}
	}
	return ids
}

// buildRecallPrompt constructs the user prompt for the recall pilot LLM.
func buildRecallPrompt(message string, candidates []SearchResult, backfillIDs []int64) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "## 사용자 질문\n%s\n\n", message)

	sb.WriteString("## 검색된 팩트 (ID, 카테고리, 중요도, 내용)\n")
	for _, sr := range candidates {
		date := sr.Fact.CreatedAt.Format("2006-01-02")
		fmt.Fprintf(&sb, "- id:%d [%s] [%.1f] (%s) %s\n",
			sr.Fact.ID, sr.Fact.Category, sr.Fact.Importance, date, sr.Fact.Content)
	}

	if len(backfillIDs) > 0 {
		sb.WriteString("\n## 빈 팩트 (엔티티/관계 미등록 — backfill 대상)\n")
		sb.WriteString("빈 팩트 ID: [")
		for i, id := range backfillIDs {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%d", id)
		}
		sb.WriteString("]\n")
	}

	return sb.String()
}

// processBackfill saves backfill entity/relation data asynchronously.
// Best-effort: errors are logged but never propagated.
func processBackfill(store *Store, data *BackfillData, logger *slog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("backfill: goroutine panicked", "panic", r)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, e := range data.Entities {
		if e.Name == "" || e.FactID <= 0 {
			continue
		}
		entityType := e.Type
		if entityType == "" {
			entityType = EntityUnknown
		}
		entityID, err := store.UpsertEntity(ctx, e.Name, entityType)
		if err != nil {
			logger.Debug("backfill: upsert entity failed", "name", e.Name, "error", err)
			continue
		}
		role := e.Role
		if role == "" {
			role = "mentioned"
		}
		if err := store.LinkFactEntity(ctx, e.FactID, entityID, role); err != nil {
			logger.Debug("backfill: link entity failed", "fact_id", e.FactID, "entity", e.Name, "error", err)
		}
	}

	for _, r := range data.Relations {
		if r.FromID <= 0 || r.ToID <= 0 || r.Type == "" {
			continue
		}
		confidence := r.Confidence
		if confidence <= 0 {
			confidence = 0.7
		}
		if err := store.InsertRelation(ctx, r.FromID, r.ToID, r.Type, confidence); err != nil {
			logger.Debug("backfill: insert relation failed", "from", r.FromID, "to", r.ToID, "error", err)
		}
	}

	logger.Info("recall: backfill complete",
		"entities", len(data.Entities),
		"relations", len(data.Relations))
}

// formatRecallResult converts a RecallResult into knowledge text for system prompt injection.
func formatRecallResult(result *RecallResult, candidates []SearchResult) string {
	if len(result.Facts) == 0 {
		return formatCandidatesAsKnowledge(candidates)
	}

	// Build a lookup of candidate facts by ID.
	factByID := make(map[int64]SearchResult, len(candidates))
	for _, sr := range candidates {
		factByID[sr.Fact.ID] = sr
	}

	var sb strings.Builder
	sb.WriteString("### 메모리 (recall)\n")

	for _, rf := range result.Facts {
		sr, ok := factByID[rf.ID]
		if !ok {
			// Fact not in candidate pool — skip to avoid LLM-hallucinated IDs
			// pulling in unrelated facts.
			continue
		}
		date := sr.Fact.CreatedAt.Format("2006-01-02")
		fmt.Fprintf(&sb, "- [%.1f] {%s} (%s) %s", sr.Fact.Importance, sr.Fact.Category, date, sr.Fact.Content)
		if rf.Reason != "" {
			fmt.Fprintf(&sb, " — %s", rf.Reason)
		}
		sb.WriteString("\n")
	}

	// Entity summaries.
	if len(result.EntitySummary) > 0 {
		sb.WriteString("\n### 엔티티 요약\n")
		for name, summary := range result.EntitySummary {
			fmt.Fprintf(&sb, "- **%s**: %s\n", name, summary)
		}
	}

	// Timeline.
	if result.Timeline != "" {
		fmt.Fprintf(&sb, "\n### 타임라인\n%s\n", result.Timeline)
	}

	return sb.String()
}

// formatCandidatesAsKnowledge is the fallback formatter when pilot LLM fails.
// Produces the same format as the standard knowledge prefetch.
func formatCandidatesAsKnowledge(candidates []SearchResult) string {
	if len(candidates) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("### 메모리\n")
	for _, sr := range candidates {
		date := sr.Fact.CreatedAt.Format("2006-01-02")
		fmt.Fprintf(&sb, "- [%.1f] {%s} (%s) %s\n",
			sr.Fact.Importance, sr.Fact.Category, date, sr.Fact.Content)
	}
	return sb.String()
}
