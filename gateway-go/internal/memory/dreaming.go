// dreaming.go — AuroraDream: periodic memory consolidation inspired by Honcho's "Dreaming" feature.
// Runs every 50 turns or 8 hours to:
//   0. Clean up expired facts
//   1. Verify existing facts (still valid?)
//   2. Merge duplicate/similar facts
//   3. Extract meta-patterns (inductive reasoning)
//   4. Resolve contradictions between facts
//   5. Update the user model
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Dreaming configuration.
const (
	DreamingTurnThreshold  = 50
	DreamingTimeIntervalH  = 8
	dreamingTimeout        = 5 * time.Minute
	dreamingBatchSize      = 20
	dreamingMaxTokens      = 512
	similarityMergeThreshold = 0.85
)

// DreamingReport summarizes the results of a dreaming cycle.
type DreamingReport struct {
	FactsVerified     int           `json:"facts_verified"`
	FactsMerged       int           `json:"facts_merged"`
	FactsExpired      int           `json:"facts_expired"`
	PatternsExtracted int           `json:"patterns_extracted"`
	Duration          time.Duration `json:"duration"`
}

// RunDreamingCycle executes a full dreaming cycle: verify → merge → extract → update.
func RunDreamingCycle(ctx context.Context, store *Store, embedder *Embedder, client *llm.Client, model string, logger *slog.Logger) (*DreamingReport, error) {
	ctx, cancel := context.WithTimeout(ctx, dreamingTimeout)
	defer cancel()

	start := time.Now()
	report := &DreamingReport{}

	logger.Info("aurora-dream: starting cycle")

	// Phase 0: Clean up expired facts (by expires_at date).
	if expiredCount, err := store.CleanupExpired(ctx); err == nil && expiredCount > 0 {
		logger.Info("aurora-dream: cleaned up expired facts", "count", expiredCount)
		report.FactsExpired += int(expiredCount)
	}

	// Phase 1: Fact verification.
	verified, expired, err := verifyFacts(ctx, store, client, model, logger)
	if err != nil {
		logger.Warn("aurora-dream: verification phase failed", "error", err)
	} else {
		report.FactsVerified = verified
		report.FactsExpired = expired
	}

	// Phase 2: Duplicate merging.
	merged, err := mergeDuplicates(ctx, store, embedder, client, model, logger)
	if err != nil {
		logger.Warn("aurora-dream: merge phase failed", "error", err)
	} else {
		report.FactsMerged = merged
	}

	// Phase 3: Pattern extraction.
	patterns, err := extractPatterns(ctx, store, client, model, logger)
	if err != nil {
		logger.Warn("aurora-dream: pattern extraction failed", "error", err)
	} else {
		report.PatternsExtracted = patterns
	}

	// Phase 4: Conflict resolution (Honcho-style).
	// Identify contradicting facts and resolve them via LLM.
	conflicts, err := resolveConflicts(ctx, store, client, model, logger)
	if err != nil {
		logger.Warn("aurora-dream: conflict resolution failed", "error", err)
	} else if conflicts > 0 {
		report.FactsMerged += conflicts
	}

	// Phase 5: User model update.
	if err := updateUserModel(ctx, store, client, model, logger); err != nil {
		logger.Warn("aurora-dream: user model update failed", "error", err)
	}

	// Phase 6: Mutual understanding synthesis (상호 인식).
	if err := synthesizeMutualUnderstanding(ctx, store, client, model, logger); err != nil {
		logger.Warn("aurora-dream: mutual understanding synthesis failed", "error", err)
	}

	report.Duration = time.Since(start)

	// Log the dreaming cycle.
	_ = store.InsertDreamingLog(ctx, DreamingLogEntry{
		RanAt:             start,
		FactsVerified:     report.FactsVerified,
		FactsMerged:       report.FactsMerged,
		FactsExpired:      report.FactsExpired,
		PatternsExtracted: report.PatternsExtracted,
		DurationMs:        report.Duration.Milliseconds(),
	})

	logger.Info("aurora-dream: cycle complete",
		"verified", report.FactsVerified,
		"merged", report.FactsMerged,
		"expired", report.FactsExpired,
		"patterns", report.PatternsExtracted,
		"duration", report.Duration.Round(time.Second),
	)

	return report, nil
}

// --- Phase 1: Fact Verification ---

const verifySystemPrompt = `You are a memory fact verifier performing "dreaming" consolidation.
Given stored facts, determine validity using these criteria:

1. **Temporal validity**: Is this fact still current? Technology choices, versions, and project states change.
2. **Logical consistency**: Does this fact contradict newer information?
3. **Relevance decay**: Is this fact about a completed/abandoned task?
4. **Confidence calibration**: Was the original importance score accurate?

Return a JSON array:
- "id": fact ID
- "valid": true/false
- "reason": brief Korean explanation if invalid
- "new_importance": (optional) adjusted importance if the score should change
Return ONLY valid JSON array, no markdown fences.`

func verifyFacts(ctx context.Context, store *Store, client *llm.Client, model string, logger *slog.Logger) (verified int, expired int, err error) {
	facts, err := store.GetFactsForDreaming(ctx)
	if err != nil {
		return 0, 0, err
	}

	// Process in batches.
	for i := 0; i < len(facts); i += dreamingBatchSize {
		end := i + dreamingBatchSize
		if end > len(facts) {
			end = len(facts)
		}
		batch := facts[i:end]

		v, e, batchErr := verifyBatch(ctx, store, client, model, batch, logger)
		if batchErr != nil {
			logger.Debug("aurora-dream: verify batch failed", "error", batchErr)
			continue
		}
		verified += v
		expired += e
	}

	return verified, expired, nil
}

func verifyBatch(ctx context.Context, store *Store, client *llm.Client, model string, batch []Fact, logger *slog.Logger) (int, int, error) {
	// Build prompt with fact list.
	var sb strings.Builder
	for _, f := range batch {
		fmt.Fprintf(&sb, "ID %d [%s, importance=%.1f]: %s\n", f.ID, f.Category, f.Importance, f.Content)
	}

	resp, err := callLLM(ctx, client, model, verifySystemPrompt, sb.String(), dreamingMaxTokens)
	if err != nil {
		return 0, 0, err
	}

	var results []struct {
		ID            int64   `json:"id"`
		Valid         bool    `json:"valid"`
		Reason        string  `json:"reason"`
		NewImportance float64 `json:"new_importance,omitempty"`
	}
	if err := json.Unmarshal([]byte(stripCodeFences(resp)), &results); err != nil {
		return 0, 0, fmt.Errorf("parse verify response: %w", err)
	}

	verified, expired := 0, 0
	for _, r := range results {
		if r.Valid {
			_ = store.MarkVerified(ctx, r.ID)
			// Adjust importance if the LLM suggested a new value.
			if r.NewImportance > 0 && r.NewImportance <= 1.0 {
				_ = store.UpdateImportance(ctx, r.ID, r.NewImportance)
			}
			verified++
		} else {
			_ = store.DeactivateFact(ctx, r.ID)
			expired++
			logger.Info("aurora-dream: expired fact", "id", r.ID, "reason", r.Reason)
		}
	}

	return verified, expired, nil
}

// --- Phase 2: Duplicate Merging ---

const mergeSystemPrompt = `You are a memory deduplication assistant.
Given two similar facts, merge them into one concise fact that captures all information.
Return a JSON object:
- "merged_content": the merged fact (Korean, concise)
- "category": the best category for the merged fact
- "importance": importance score (0.0-1.0)
Return ONLY valid JSON, no markdown fences.`

func mergeDuplicates(ctx context.Context, store *Store, embedder *Embedder, client *llm.Client, model string, logger *slog.Logger) (int, error) {
	if embedder == nil {
		return 0, nil
	}

	embeddings, err := store.LoadEmbeddings(ctx)
	if err != nil {
		return 0, err
	}

	// Find similar pairs above threshold.
	type pair struct{ a, b int64 }
	var pairs []pair

	ids := make([]int64, 0, len(embeddings))
	for id := range embeddings {
		ids = append(ids, id)
	}

	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			sim := cosineSimilarity(embeddings[ids[i]], embeddings[ids[j]])
			if sim >= similarityMergeThreshold {
				pairs = append(pairs, pair{ids[i], ids[j]})
			}
		}
	}

	if len(pairs) == 0 {
		return 0, nil
	}

	merged := 0
	// Limit merges per cycle to avoid excessive LLM calls.
	maxMerges := 10
	for _, p := range pairs {
		if merged >= maxMerges {
			break
		}

		factA, err := store.GetFact(ctx, p.a)
		if err != nil || !factA.Active {
			continue
		}
		factB, err := store.GetFact(ctx, p.b)
		if err != nil || !factB.Active {
			continue
		}

		prompt := fmt.Sprintf("Fact A: %s\nFact B: %s", factA.Content, factB.Content)
		resp, err := callLLM(ctx, client, model, mergeSystemPrompt, prompt, 256)
		if err != nil {
			continue
		}

		var result struct {
			MergedContent string  `json:"merged_content"`
			Category      string  `json:"category"`
			Importance    float64 `json:"importance"`
		}
		if err := json.Unmarshal([]byte(stripCodeFences(resp)), &result); err != nil {
			continue
		}

		if result.MergedContent == "" {
			continue
		}
		if !isValidCategory(result.Category) {
			result.Category = factA.Category
		}

		// Insert merged fact.
		newID, err := store.InsertFact(ctx, Fact{
			Content:    result.MergedContent,
			Category:   result.Category,
			Importance: clamp(result.Importance, 0, 1),
			Source:     SourceDreaming,
		})
		if err != nil {
			continue
		}

		// Supersede old facts.
		_ = store.SupersedeFact(ctx, p.a, newID)
		_ = store.SupersedeFact(ctx, p.b, newID)

		// Embed the new fact.
		if embedder != nil {
			_ = embedder.EmbedAndStore(ctx, newID, result.MergedContent)
		}

		merged++
		logger.Info("aurora-dream: merged facts", "old_a", p.a, "old_b", p.b, "new", newID)
	}

	return merged, nil
}

// --- Phase 4: Conflict Resolution (Honcho-style) ---

const conflictSystemPrompt = `You are a fact conflict resolution assistant.
Given a list of facts in the same category, identify contradictions or superseded information.
For each conflict found, return a JSON array of objects:
- "keep_id": the fact ID to keep (more recent or more accurate)
- "remove_id": the fact ID to deactivate
- "reason": brief explanation (Korean)
If no conflicts found, return [].
Return ONLY valid JSON array, no markdown fences.`

func resolveConflicts(ctx context.Context, store *Store, client *llm.Client, model string, logger *slog.Logger) (int, error) {
	facts, err := store.GetActiveFacts(ctx)
	if err != nil || len(facts) < 5 {
		return 0, err
	}

	// Group by category and check for conflicts within each group.
	categories := map[string][]Fact{}
	for _, f := range facts {
		categories[f.Category] = append(categories[f.Category], f)
	}

	resolved := 0
	for cat, catFacts := range categories {
		if len(catFacts) < 3 {
			continue // too few to have conflicts
		}

		var sb strings.Builder
		limit := 20
		if len(catFacts) < limit {
			limit = len(catFacts)
		}
		for _, f := range catFacts[:limit] {
			fmt.Fprintf(&sb, "ID %d [importance=%.1f, %s]: %s\n",
				f.ID, f.Importance, f.CreatedAt.Format("2006-01-02"), f.Content)
		}

		resp, err := callLLM(ctx, client, model, conflictSystemPrompt, fmt.Sprintf("Category: %s\n\n%s", cat, sb.String()), dreamingMaxTokens)
		if err != nil {
			continue
		}

		var results []struct {
			KeepID   int64  `json:"keep_id"`
			RemoveID int64  `json:"remove_id"`
			Reason   string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(stripCodeFences(resp)), &results); err != nil {
			continue
		}

		for _, r := range results {
			if r.KeepID > 0 && r.RemoveID > 0 && r.KeepID != r.RemoveID {
				_ = store.SupersedeFact(ctx, r.RemoveID, r.KeepID)
				resolved++
				logger.Info("aurora-dream: resolved conflict", "keep", r.KeepID, "remove", r.RemoveID, "reason", r.Reason)
			}
		}
	}

	return resolved, nil
}

// --- Phase 3: Pattern Extraction ---

const patternSystemPrompt = `You are a meta-reasoning engine performing "dreaming" pattern extraction.
This is the INDUCTIVE reasoning phase: from many specific observations, derive general patterns.

Given accumulated facts, perform:
1. **Pattern Induction**: What recurring themes emerge across multiple facts?
2. **Behavioral Modeling**: What work habits, expertise areas, or decision patterns are visible?
3. **Hypothesis Formation**: What predictions can you make about future behavior?

Return a JSON array of discovered patterns:
- "content": the pattern (Korean, concise, evidence-based)
- "category": "user_model"
- "importance": 0.8-1.0 (patterns are high-value by definition)
If no clear patterns (< 3 supporting facts), return [].
Return ONLY valid JSON array, no markdown fences.`

func extractPatterns(ctx context.Context, store *Store, client *llm.Client, model string, logger *slog.Logger) (int, error) {
	facts, err := store.GetActiveFacts(ctx)
	if err != nil {
		return 0, err
	}

	if len(facts) < 10 {
		// Not enough facts to extract meaningful patterns.
		return 0, nil
	}

	// Group by category.
	var sb strings.Builder
	categories := map[string][]Fact{}
	for _, f := range facts {
		categories[f.Category] = append(categories[f.Category], f)
	}

	for cat, catFacts := range categories {
		fmt.Fprintf(&sb, "\n[%s] (%d facts):\n", cat, len(catFacts))
		limit := 15
		if len(catFacts) < limit {
			limit = len(catFacts)
		}
		for _, f := range catFacts[:limit] {
			fmt.Fprintf(&sb, "- %s\n", f.Content)
		}
	}

	resp, err := callLLM(ctx, client, model, patternSystemPrompt, sb.String(), dreamingMaxTokens)
	if err != nil {
		return 0, err
	}

	var patterns []ExtractedFact
	if err := json.Unmarshal([]byte(stripCodeFences(resp)), &patterns); err != nil {
		return 0, nil
	}

	count := 0
	for _, p := range patterns {
		if p.Content == "" {
			continue
		}
		_, err := store.InsertFact(ctx, Fact{
			Content:    p.Content,
			Category:   CategoryUserModel,
			Importance: clamp(p.Importance, 0.7, 1.0),
			Source:     SourceDreaming,
		})
		if err == nil {
			count++
		}
	}

	return count, nil
}

// --- Phase 4: User Model Update ---

const userModelSystemPrompt = `You are a deep user profile synthesizer for a personal AI assistant.
Given facts about a user across all categories, synthesize a rich, evidence-based profile.

## Profile Keys

- communication_style: 소통 스타일 — 선호하는 답변 길이, 형식성 수준, 유머 사용 여부, 설명 깊이 선호도. 예: "간결한 답변 선호, 불릿 포인트 형식, 캐주얼한 톤, 불필요한 설명 싫어함"
- expertise_areas: 전문 영역 — 깊은 전문성 vs 얕은 관심사 구분. 예: "Go/Rust 깊은 전문성, 인프라(DGX/CUDA) 실무 수준, ML 이론보다 실용 중심"
- tech_preferences: 기술 선호 — 선호하는 도구, 프레임워크, 아키텍처 패턴, 기피하는 기술. 예: "SQLite > PostgreSQL, 단순한 구조 선호, 불필요한 추상화 기피"
- common_tasks: 주요 작업 — 자주 요청하는 작업 유형과 패턴. 예: "Go/Rust 코드 작성, 시스템 설계, 버그 디버깅, 문서 작성은 거의 안 함"
- work_patterns: 작업 패턴 — 작업 리듬, 멀티태스킹 성향, 맥락 전환 패턴. 예: "깊은 집중 세션, 동시에 여러 에이전트 활용, 야간 작업 빈번"

## Rules
- All values in Korean
- Be SPECIFIC and evidence-based (cite the pattern, not generic descriptions)
- "X를 선호함" 보다 "3번의 대화에서 일관되게 X를 선택함 — Y 이유로 추정" 식의 근거 기반 서술
- If a key cannot be determined from available data, omit it
Return ONLY valid JSON object, no markdown fences.`

func updateUserModel(ctx context.Context, store *Store, client *llm.Client, model string, logger *slog.Logger) error {
	facts, err := store.GetActiveFacts(ctx)
	if err != nil {
		return err
	}

	if len(facts) < 5 {
		return nil // not enough data
	}

	var sb strings.Builder
	limit := 30
	if len(facts) < limit {
		limit = len(facts)
	}
	for _, f := range facts[:limit] {
		fmt.Fprintf(&sb, "[%s] %s\n", f.Category, f.Content)
	}

	resp, err := callLLM(ctx, client, model, userModelSystemPrompt, sb.String(), dreamingMaxTokens)
	if err != nil {
		return err
	}

	var profile map[string]string
	if err := json.Unmarshal([]byte(stripCodeFences(resp)), &profile); err != nil {
		return nil // non-fatal
	}

	for key, value := range profile {
		if value == "" {
			continue
		}
		if err := store.SetUserModel(ctx, key, value, 0.8); err != nil {
			logger.Debug("aurora-dream: failed to set user model", "key", key, "error", err)
		}
	}

	logger.Info("aurora-dream: updated user model", "keys", len(profile))
	return nil
}

// --- Phase 6: Mutual Understanding Synthesis (상호 인식) ---
//
// Unlike Phase 5 (static user profile), Phase 6 tracks the EVOLVING relationship:
// - Reads previous mutual understanding state for continuity
// - Analyzes new mutual signals since last cycle
// - Produces updated understanding that reflects temporal changes
// - Cleans up consumed mu_signals_raw after synthesis

const mutualUnderstandingSystemPrompt = `You are an AI-user relationship analyst performing "dreaming" mutual understanding synthesis.
You will receive TWO inputs:
1. **이전 상태**: The previous mutual understanding state (may be empty on first run)
2. **새로운 시그널**: Recently accumulated relationship signals and contextual facts

Your job: EVOLVE the understanding — don't start from scratch. Build on the previous state,
incorporate new signals, and note what CHANGED.

## Analysis Framework

### 사용자 → AI 인식 (user_sees_ai)
Synthesize how the user perceives the AI. Look for:
- Satisfaction trajectory: improving, declining, or stable? Why?
- Trust level: does user verify AI output, or delegate freely?
- Unmet expectations: what does the user want that the AI isn't delivering?
- Emotional tone: warm/collaborative, neutral/transactional, or frustrated/distant?

### AI → 사용자 이해 (ai_understands_user)
Synthesize the AI's accumulated understanding of the user. Include:
- Core personality traits (communication style, decision-making approach)
- Expertise depth map (what they know deeply vs superficially)
- Emotional patterns (what triggers frustration, what brings satisfaction)
- Work rhythm (when they work, how they context-switch, attention patterns)

### 관계 역학 (relationship_dynamics)
Analyze the relationship trajectory:
- Rapport trend: deepening, plateauing, or degrading?
- Communication efficiency: is less explanation needed over time?
- Shared context growth: inside references, assumed knowledge
- Power dynamic: does user lead, collaborate, or delegate?

### 적응 메모 (adaptation_notes)
CONCRETE behavioral directives for the AI. Not vague — specific and actionable:
- "사용자가 X를 물을 때 Y 방식으로 답변할 것" (not "더 잘 답변할 것")
- "Z 상황에서는 확인 없이 바로 실행할 것" (trust-based delegation)
- "W 주제는 간결하게, V 주제는 상세하게" (topic-specific adaptation)

## Output Format
Return a JSON object (Korean values, 2-4 sentences per key):
- "user_sees_ai": "..."
- "ai_understands_user": "..."
- "relationship_dynamics": "..."
- "adaptation_notes": "..."

If a previous state exists, note what evolved (e.g., "이전보다 신뢰가 높아짐: ~").
If insufficient data for a key, omit it.
Return ONLY valid JSON object, no markdown fences.`

func synthesizeMutualUnderstanding(ctx context.Context, store *Store, client *llm.Client, model string, logger *slog.Logger) error {
	facts, err := store.GetActiveFacts(ctx)
	if err != nil {
		return err
	}

	if len(facts) < 5 {
		return nil // not enough data to synthesize
	}

	// Load previous mutual understanding state for continuity.
	prevState := loadPreviousMutualState(ctx, store)

	// Gather relevant facts with priority ordering.
	var sb strings.Builder

	// Section 1: Previous state (if exists).
	if prevState != "" {
		fmt.Fprintf(&sb, "## 이전 상태\n%s\n\n", prevState)
	}

	// Section 2: New mutual signals (highest priority).
	sb.WriteString("## 새로운 시그널\n")
	mutualFacts := 0
	for _, f := range facts {
		if f.Category == CategoryMutual && mutualFacts < 25 {
			fmt.Fprintf(&sb, "[mutual, %.1f, %s] %s\n",
				f.Importance, f.CreatedAt.Format("01-02"), f.Content)
			mutualFacts++
		}
	}

	// Section 3: Raw accumulated signals (from between dreaming cycles).
	entries, _ := store.GetUserModel(ctx)
	for _, e := range entries {
		if e.Key == "mu_signals_raw" && e.Value != "" {
			fmt.Fprintf(&sb, "\n## 미처리 시그널\n%s\n", e.Value)
			break
		}
	}

	// Section 4: Supporting context from other categories.
	sb.WriteString("\n## 맥락\n")
	supportCats := map[string]bool{CategoryPreference: true, CategoryUserModel: true, CategoryDecision: true}
	support := 0
	for _, f := range facts {
		if supportCats[f.Category] && support < 15 {
			fmt.Fprintf(&sb, "[%s, %.1f] %s\n", f.Category, f.Importance, f.Content)
			support++
		}
	}

	if mutualFacts == 0 && prevState == "" && support < 5 {
		return nil // not enough data
	}

	// Use higher token budget for richer synthesis.
	resp, err := callLLM(ctx, client, model, mutualUnderstandingSystemPrompt, sb.String(), 768)
	if err != nil {
		return err
	}

	var profile map[string]string
	if err := json.Unmarshal([]byte(stripCodeFences(resp)), &profile); err != nil {
		return nil // non-fatal
	}

	mutualKeys := map[string]bool{
		"user_sees_ai":          true,
		"ai_understands_user":   true,
		"relationship_dynamics": true,
		"adaptation_notes":      true,
	}

	updated := 0
	for key, value := range profile {
		if value == "" || !mutualKeys[key] {
			continue
		}
		if err := store.SetUserModel(ctx, key, value, 0.85); err != nil {
			logger.Debug("aurora-dream: failed to set mutual understanding", "key", key, "error", err)
		} else {
			updated++
		}
	}

	// Clear consumed mu_signals_raw after successful synthesis.
	if updated > 0 {
		_ = store.SetUserModel(ctx, "mu_signals_raw", "", 0)
		logger.Info("aurora-dream: updated mutual understanding", "keys", updated, "signals_consumed", mutualFacts)
	}

	return nil
}

// loadPreviousMutualState reads the current mutual understanding keys
// and formats them as context for the next synthesis cycle.
func loadPreviousMutualState(ctx context.Context, store *Store) string {
	entries, err := store.GetUserModel(ctx)
	if err != nil {
		return ""
	}

	labels := map[string]string{
		"user_sees_ai":          "사용자 → AI 인식",
		"ai_understands_user":   "AI → 사용자 이해",
		"relationship_dynamics": "관계 역학",
		"adaptation_notes":      "적응 메모",
	}

	var sb strings.Builder
	for _, e := range entries {
		label, ok := labels[e.Key]
		if !ok || e.Value == "" {
			continue
		}
		fmt.Fprintf(&sb, "- %s: %s\n", label, e.Value)
	}
	return sb.String()
}

// callLLM is a convenience alias for callSglang (defined in sglang.go).
func callLLM(ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (string, error) {
	return callSglang(ctx, client, model, system, user, maxTokens)
}
