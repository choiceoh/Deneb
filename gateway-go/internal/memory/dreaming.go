// dreaming.go — AuroraDream: periodic memory consolidation inspired by Honcho's "Dreaming" feature.
// Runs every 50 turns, 8 hours, or when active facts exceed 200 to:
//  0. Clean up expired facts
//  1. Verify existing facts + resolve contradictions (unified LLM call)
//  2. Merge duplicate/similar facts
//  3. Extract meta-patterns (inductive reasoning)
//  4. Update the user model
//  5. Synthesize mutual understanding
package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Dreaming configuration.
const (
	DreamingTurnThreshold    = 50
	DreamingTimeIntervalH    = 8
	dreamingTimeout          = 15 * time.Minute
	dreamingBatchSize        = 50
	dreamingMaxTokens        = 1024
	similarityMergeThreshold = 0.78
	maxMergeDepth            = 2 // cascade prevention: facts at this depth are ineligible for merging

	// Per-phase timeouts prevent earlier phases from starving later ones.
	// If a phase exceeds its budget, it's cut short but subsequent phases still run.
	// Sum ~15m; the overall dreamingTimeout acts as a hard ceiling.
	phaseTimeoutVerify    = 3 * time.Minute
	phaseTimeoutMerge     = 3 * time.Minute
	phaseTimeoutPatterns  = 150 * time.Second
	phaseTimeoutUserModel = 2 * time.Minute
	phaseTimeoutMutual    = 2 * time.Minute
)

// DreamingReport summarizes the results of a dreaming cycle.
type DreamingReport struct {
	FactsVerified     int           `json:"facts_verified"`
	FactsMerged       int           `json:"facts_merged"`
	FactsExpired      int           `json:"facts_expired"`
	FactsPruned       int           `json:"facts_pruned"`
	PatternsExtracted int           `json:"patterns_extracted"`
	PhaseErrors       []string      `json:"phase_errors,omitempty"`
	Duration          time.Duration `json:"duration"`
}

// dreamState holds all shared dependencies and the accumulated report for a
// single dreaming cycle. Phases receive the full state rather than individual
// parameters, avoiding repetitive function signatures.
type dreamState struct {
	store    *Store
	embedder *Embedder
	client   *llm.Client
	model    string
	logger   *slog.Logger
	report   *DreamingReport
}

// dreamPhase is the interface implemented by every dreaming phase.
// Each phase has its own per-phase timeout; errors are logged and do not
// block subsequent phases.
type dreamPhase interface {
	Name() string
	Run(ctx context.Context, state *dreamState) error
}

// runPhase executes phase with its own timeout budget carved from ctx.
// Phase failures are logged as warnings; subsequent phases always run.
func runPhase(ctx context.Context, budget time.Duration, phase dreamPhase, state *dreamState) {
	pCtx, pCancel := phaseContext(ctx, budget)
	defer pCancel()
	if err := phase.Run(pCtx, state); err != nil {
		state.logger.Warn("aurora-dream: phase failed", "phase", phase.Name(), "error", err)
		state.report.PhaseErrors = append(state.report.PhaseErrors, phase.Name()+": "+err.Error())
	}
}

// RunDreamingCycle executes a full dreaming cycle: verify → merge → extract → resolve → update.
func RunDreamingCycle(ctx context.Context, store *Store, embedder *Embedder, client *llm.Client, model string, logger *slog.Logger) (*DreamingReport, error) {
	ctx, cancel := context.WithTimeout(ctx, dreamingTimeout)
	defer cancel()

	start := time.Now()
	state := &dreamState{
		store:    store,
		embedder: embedder,
		client:   client,
		model:    model,
		logger:   logger,
		report:   &DreamingReport{},
	}

	activeCount, _ := store.CountActiveFacts(ctx)
	logger.Info("aurora-dream: starting cycle", "active_facts", activeCount)

	// Phase 0: Clean up expired facts (by expires_at date).
	if expiredCount, err := store.CleanupExpired(ctx); err == nil && expiredCount > 0 {
		logger.Info("aurora-dream: cleaned up expired facts", "count", expiredCount)
		state.report.FactsExpired += int(expiredCount)
	}

	// Phase 0.5: Prune low-importance noise (context/auto_extract, unaccessed, unverified, >14 days).
	// Threshold lowered to 0.45 and age raised to 14d to preserve factual context
	// records that may have moderate importance but carry project state.
	if pruned, err := store.PruneNoiseFacts(ctx, 0.45, 14*24*time.Hour); err == nil && pruned > 0 {
		logger.Info("aurora-dream: pruned noise facts", "count", pruned)
		state.report.FactsPruned = int(pruned)
	}

	// Phase 0.75: Retry pending embeddings (facts that failed async embedding).
	if embedder != nil {
		if n, err := store.RetryPendingEmbeddings(ctx, embedder.EmbedAndStore); err == nil && n > 0 {
			logger.Info("aurora-dream: retried pending embeddings", "count", n)
		}
	}

	// Phases 1–6: each gets its own timeout budget; the outer ctx is the hard ceiling.
	// Conflict resolution is merged into the verify phase (single LLM call per batch),
	// so there is no standalone conflict phase.
	phases := []struct {
		phase  dreamPhase
		budget time.Duration
	}{
		{verifyPhase{}, phaseTimeoutVerify},
		{mergePhase{}, phaseTimeoutMerge},
		{patternPhase{}, phaseTimeoutPatterns},
		{userModelPhase{}, phaseTimeoutUserModel},
		{mutualPhase{}, phaseTimeoutMutual},
	}

	for _, p := range phases {
		runPhase(ctx, p.budget, p.phase, state)
	}

	state.report.Duration = time.Since(start)

	// Log the dreaming cycle.
	_ = store.InsertDreamingLog(ctx, DreamingLogEntry{
		RanAt:             start,
		FactsVerified:     state.report.FactsVerified,
		FactsMerged:       state.report.FactsMerged,
		FactsExpired:      state.report.FactsExpired,
		FactsPruned:       state.report.FactsPruned,
		PatternsExtracted: state.report.PatternsExtracted,
		DurationMs:        state.report.Duration.Milliseconds(),
	})

	logger.Info("aurora-dream: cycle complete",
		"verified", state.report.FactsVerified,
		"merged", state.report.FactsMerged,
		"expired", state.report.FactsExpired,
		"pruned", state.report.FactsPruned,
		"patterns", state.report.PatternsExtracted,
		"duration", state.report.Duration.Round(time.Second),
	)

	return state.report, nil
}

// phaseContext returns a child context with the shorter of the per-phase
// timeout and the parent's remaining deadline. This ensures each phase gets
// its own budget while still respecting the overall dreaming deadline.
func phaseContext(parent context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := parent.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < budget {
			budget = remaining
		}
	}
	return context.WithTimeout(parent, budget)
}

// --- Phase 1: Fact Verification ---

type verifyPhase struct{}

func (verifyPhase) Name() string { return "verify" }
func (verifyPhase) Run(ctx context.Context, s *dreamState) error {
	verified, expired, conflicts, err := verifyFacts(ctx, s.store, s.client, s.model, s.logger)
	if err != nil {
		return err
	}
	s.report.FactsVerified = verified
	s.report.FactsExpired += expired
	s.report.FactsMerged += conflicts
	return nil
}

// verifyAndResolveSystemPrompt combines fact verification (Phase 1) and conflict
// resolution (Phase 4) into a single LLM call per batch. This halves the total
// LLM calls compared to running the two phases separately.
const verifyAndResolveSystemPrompt = `You are a memory fact verifier performing "dreaming" consolidation.
Given stored facts, perform TWO tasks in a single pass:

## Task 1: Fact Verification
Determine each fact's validity using these criteria:
1. **Temporal validity**: Is this fact still current? Technology choices, versions, and project states change.
2. **Logical consistency**: Does this fact contradict newer information?
3. **Relevance decay**: Is this fact about a completed/abandoned task?
4. **Confidence calibration**: Was the original importance score accurate?

## Task 2: Conflict Resolution
Within the SAME category, identify genuine contradictions — facts that are mutually incompatible and cannot both be true.
- Only flag TRUE contradictions (logically incompatible facts). Do NOT flag duplicates, overlapping, or complementary facts.
- For each conflict, keep the fact that is more specific, has higher importance, or is more recent.
- Each remove_id must appear at most ONCE across all conflicts.
- Only consider facts you marked as valid in Task 1.

Return a JSON object with both arrays:
{"results": [{"id": <fact ID>, "valid": true/false, "reason": "brief Korean explanation if invalid", "new_importance": <optional adjusted importance>}, ...], "conflicts": [{"keep_id": <ID>, "remove_id": <ID>, "reason": "brief Korean explanation"}, ...]}
If no conflicts exist, return an empty "conflicts" array.
Return ONLY valid JSON.`

type verifyResult struct {
	ID            int64   `json:"id"`
	Valid         bool    `json:"valid"`
	Reason        string  `json:"reason"`
	NewImportance float64 `json:"new_importance,omitempty"`
}

type verifyAndResolveResponse struct {
	Results   []verifyResult   `json:"results"`
	Conflicts []conflictResult `json:"conflicts"`
}

func verifyFacts(ctx context.Context, store *Store, client *llm.Client, model string, logger *slog.Logger) (verified int, expired int, conflictsResolved int, err error) {
	facts, err := store.GetFactsForDreaming(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	logger.Info("aurora-dream: verify phase input", "facts_to_verify", len(facts))

	removed := map[int64]bool{} // track removed IDs across batches

	// Process in batches.
	for i := 0; i < len(facts); i += dreamingBatchSize {
		end := i + dreamingBatchSize
		if end > len(facts) {
			end = len(facts)
		}
		batch := facts[i:end]

		v, e, c, batchErr := verifyAndResolveBatch(ctx, store, client, model, batch, removed, logger)
		if batchErr != nil {
			if shouldStopVerifyBatches(batchErr) {
				logger.Debug("aurora-dream: stopping verify phase due to context deadline", "error", batchErr)
				break
			}
			logger.Debug("aurora-dream: verify batch failed", "error", batchErr)
			continue
		}
		verified += v
		expired += e
		conflictsResolved += c
	}

	return verified, expired, conflictsResolved, nil
}

func shouldStopVerifyBatches(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func verifyAndResolveBatch(ctx context.Context, store *Store, client *llm.Client, model string, batch []Fact, removed map[int64]bool, logger *slog.Logger) (int, int, int, error) {
	var sb strings.Builder
	for _, f := range batch {
		fmt.Fprintf(&sb, "ID %d [%s, importance=%.1f, %s]: %s\n",
			f.ID, f.Category, f.Importance, f.CreatedAt.Format("2006-01-02"), f.Content)
	}

	wrapper, err := callLLMJSON[verifyAndResolveResponse](ctx, client, model, verifyAndResolveSystemPrompt, sb.String(), dreamingMaxTokens)
	if err != nil {
		return 0, 0, 0, err
	}

	verified, expired := 0, 0
	for _, r := range wrapper.Results {
		if r.Valid {
			if err := store.MarkVerified(ctx, r.ID); err != nil {
				logger.Warn("aurora-dream: failed to mark fact verified", "id", r.ID, "error", err)
			}
			if r.NewImportance > 0 && r.NewImportance <= 1.0 {
				if err := store.UpdateImportance(ctx, r.ID, r.NewImportance); err != nil {
					logger.Warn("aurora-dream: failed to update importance", "id", r.ID, "error", err)
				}
			}
			verified++
		} else {
			if err := store.DeactivateFact(ctx, r.ID); err != nil {
				logger.Warn("aurora-dream: failed to deactivate fact", "id", r.ID, "error", err)
			}
			removed[r.ID] = true
			expired++
			logger.Info("aurora-dream: expired fact", "id", r.ID, "reason", r.Reason)
		}
	}

	// Process conflicts detected within this batch.
	resolved := 0
	for _, c := range wrapper.Conflicts {
		if c.KeepID <= 0 || c.RemoveID <= 0 || c.KeepID == c.RemoveID {
			continue
		}
		if removed[c.RemoveID] || removed[c.KeepID] {
			continue
		}
		if err := store.SupersedeFact(ctx, c.RemoveID, c.KeepID); err != nil {
			logger.Warn("aurora-dream: failed to supersede fact", "remove", c.RemoveID, "keep", c.KeepID, "error", err)
		}
		removed[c.RemoveID] = true
		resolved++
		logger.Info("aurora-dream: resolved conflict", "keep", c.KeepID, "remove", c.RemoveID, "reason", c.Reason)
	}

	return verified, expired, resolved, nil
}

// --- Phase 2: Duplicate Merging ---

type mergePhase struct{}

func (mergePhase) Name() string { return "merge" }
func (mergePhase) Run(ctx context.Context, s *dreamState) error {
	var merged int
	var err error
	if s.embedder != nil {
		merged, err = mergeDuplicates(ctx, s.store, s.embedder, s.client, s.model, s.logger)
	} else {
		// P11/P14: text-only fallback when embedder is unavailable.
		merged, err = mergeDuplicatesTextOnly(ctx, s.store, s.logger)
	}
	if err != nil {
		return err
	}
	s.report.FactsMerged = merged
	return nil
}

const mergeSystemPrompt = `You are a memory deduplication assistant.
Given two facts, decide if they are truly duplicates (same core information, different wording).
If they describe different information (even if related or overlapping topics), set should_merge to false.
Return a JSON object:
- "should_merge": true only if facts contain the same core information
- "merged_content": the merged fact (Korean, concise) — empty string if should_merge is false
- "category": the best category for the merged fact
- "importance": importance score (0.0-1.0)
Return ONLY valid JSON, no markdown fences.`

type mergeResponse struct {
	ShouldMerge   bool    `json:"should_merge"`
	MergedContent string  `json:"merged_content"`
	Category      string  `json:"category"`
	Importance    float64 `json:"importance"`
}

// mergeMaxPerCategory caps the number of facts compared pairwise within a single
// category during merge. When a category exceeds this limit, only the highest-importance
// facts are compared — low-importance duplicates are not worth the O(n²) cost.
const mergeMaxPerCategory = 100

func mergeDuplicates(ctx context.Context, store *Store, embedder *Embedder, client *llm.Client, model string, logger *slog.Logger) (int, error) {
	embeddings, depths, categories, err := store.LoadEmbeddingsForMerge(ctx, maxMergeDepth)
	if err != nil {
		return 0, err
	}
	logger.Info("aurora-dream: merge phase input", "embeddings_loaded", len(embeddings))

	// Group IDs by category so we only compare within the same category.
	// This reduces O(n²) across all facts to O(n²/k) where k is the number
	// of categories (~6), since duplicates only make sense within a category.
	catGroups := map[string][]int64{}
	for id := range embeddings {
		cat := categories[id]
		catGroups[cat] = append(catGroups[cat], id)
	}

	// P12: Cap per-category comparison set. When a category has many facts,
	// sort by embedding vector length as a proxy for content richness and
	// limit to mergeMaxPerCategory to reduce O(n²) cost.
	for cat, ids := range catGroups {
		if len(ids) > mergeMaxPerCategory {
			// Keep first mergeMaxPerCategory IDs (already in insertion order;
			// LoadEmbeddingsForMerge returns all eligible — trim to cap).
			catGroups[cat] = ids[:mergeMaxPerCategory]
		}
	}

	// Find similar pairs above threshold (within same category and depth).
	type pair struct {
		a, b int64
		sim  float64
	}
	var pairs []pair

	for _, ids := range catGroups {
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				// Only merge facts at the same depth level to prevent
				// abstraction-level mismatch that degrades similarity.
				if depths[ids[i]] != depths[ids[j]] {
					continue
				}
				// P12: Skip pairs with vastly different vector lengths (proxy for
				// content length mismatch). Facts with 3x+ length ratio are unlikely
				// duplicates.
				vecA, vecB := embeddings[ids[i]], embeddings[ids[j]]
				if lenA, lenB := len(vecA), len(vecB); lenA > 0 && lenB > 0 {
					ratio := float64(lenA) / float64(lenB)
					if ratio > 3.0 || ratio < 1.0/3.0 {
						continue
					}
				}
				sim := cosineSimilarity(vecA, vecB)
				if sim >= similarityMergeThreshold {
					pairs = append(pairs, pair{ids[i], ids[j], sim})
				}
			}
		}
	}

	if len(pairs) == 0 {
		return 0, nil
	}

	// Process most similar pairs first so the obvious duplicates are merged
	// before hitting the per-cycle limit.
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].sim > pairs[j].sim
	})

	merged := 0
	consecutiveRejects := 0
	// Limit merges per cycle to avoid excessive LLM calls.
	maxMerges := 25
	for _, p := range pairs {
		if merged >= maxMerges {
			break
		}

		factA, err := store.GetFactReadOnly(ctx, p.a)
		if err != nil || !factA.Active {
			continue
		}
		factB, err := store.GetFactReadOnly(ctx, p.b)
		if err != nil || !factB.Active {
			continue
		}

		prompt := fmt.Sprintf("Fact A: %s\nFact B: %s", factA.Content, factB.Content)
		result, err := callLLMJSON[mergeResponse](ctx, client, model, mergeSystemPrompt, prompt, 256)
		if err != nil {
			continue
		}

		if !result.ShouldMerge || result.MergedContent == "" {
			logger.Info("aurora-dream: merge rejected by LLM", "a", p.a, "b", p.b, "sim", fmt.Sprintf("%.3f", p.sim))
			consecutiveRejects++
			if consecutiveRejects >= 3 {
				logger.Info("aurora-dream: stopping merges after consecutive rejections", "rejects", consecutiveRejects)
				break
			}
			continue
		}
		consecutiveRejects = 0
		if !isValidCategory(result.Category) {
			result.Category = factA.Category
		}

		// Compute merge depth: max of parents + 1.
		depth := factA.MergeDepth
		if factB.MergeDepth > depth {
			depth = factB.MergeDepth
		}
		depth++

		// Insert merged fact.
		newID, err := store.InsertFact(ctx, Fact{
			Content:    result.MergedContent,
			Category:   result.Category,
			Importance: clamp(result.Importance, 0, 1),
			Source:     SourceDreaming,
			MergeDepth: depth,
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
		logger.Info("aurora-dream: merged facts", "old_a", p.a, "old_b", p.b, "new", newID, "sim", fmt.Sprintf("%.3f", p.sim))
	}

	return merged, nil
}

// mergeDuplicatesTextOnly is a deterministic fallback for environments without
// an embedder. Uses Jaccard text similarity instead of cosine on embeddings.
// Only auto-merges at a very high threshold (0.90) since there's no LLM to
// validate the merge — the higher threshold prevents false positives.
const textOnlyMergeThreshold = 0.90
const textOnlyMaxMerges = 10

func mergeDuplicatesTextOnly(ctx context.Context, store *Store, logger *slog.Logger) (int, error) {
	facts, err := store.GetActiveFacts(ctx)
	if err != nil {
		return 0, err
	}
	logger.Info("aurora-dream: text-merge phase input", "active_facts", len(facts))

	// Group by category + depth (same constraints as embedding-based merge).
	type catDepthKey struct {
		category string
		depth    int
	}
	groups := map[catDepthKey][]Fact{}
	for _, f := range facts {
		if f.MergeDepth >= maxMergeDepth {
			continue
		}
		key := catDepthKey{f.Category, f.MergeDepth}
		groups[key] = append(groups[key], f)
	}

	type pair struct {
		a, b Fact
		sim  float64
	}
	var pairs []pair

	for _, group := range groups {
		// Cap per-group to avoid O(n²) blow-up.
		limit := mergeMaxPerCategory
		if len(group) < limit {
			limit = len(group)
		}
		for i := 0; i < limit; i++ {
			for j := i + 1; j < limit; j++ {
				sim := JaccardTextSimilarity(group[i].Content, group[j].Content)
				if sim >= textOnlyMergeThreshold {
					pairs = append(pairs, pair{group[i], group[j], sim})
				}
			}
		}
	}

	if len(pairs) == 0 {
		return 0, nil
	}

	// Sort by similarity descending.
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].sim > pairs[j].sim
	})

	merged := 0
	superseded := map[int64]bool{}
	for _, p := range pairs {
		if merged >= textOnlyMaxMerges {
			break
		}
		if superseded[p.a.ID] || superseded[p.b.ID] {
			continue
		}

		// Keep the fact with higher importance; supersede the other.
		keep, drop := p.a, p.b
		if p.b.Importance > p.a.Importance {
			keep, drop = p.b, p.a
		}

		_ = store.SupersedeFact(ctx, drop.ID, keep.ID)
		superseded[drop.ID] = true
		merged++
		logger.Info("aurora-dream: text-only merge", "keep", keep.ID, "drop", drop.ID,
			"jaccard", fmt.Sprintf("%.3f", p.sim))
	}

	return merged, nil
}

// --- Phase 3: Pattern Extraction ---

type patternPhase struct{}

func (patternPhase) Name() string { return "patterns" }
func (patternPhase) Run(ctx context.Context, s *dreamState) error {
	patterns, err := extractPatterns(ctx, s.store, s.embedder, s.client, s.model, s.logger)
	if err != nil {
		return err
	}
	s.report.PatternsExtracted = patterns
	return nil
}

const patternSystemPrompt = `You are a meta-reasoning engine performing "dreaming" pattern extraction.
This is the INDUCTIVE reasoning phase: from many specific observations, derive general patterns.

Given accumulated facts, perform:

1. **행동 패턴 (Behavioral)**: What work habits, expertise areas, or decision patterns are visible?
   → category: "user_model"
2. **관계 패턴 (Relational)**: What patterns exist in how the user interacts with the AI?
   - Does the user consistently correct the AI on certain topics? → adaptation needed
   - Does the user's trust level follow a pattern? (e.g., trusts for code, verifies for decisions)
   - Are there recurring frustration triggers? Recurring satisfaction sources?
   - How does the user's communication style shift based on context? (urgent vs relaxed)
   → category: "mutual"
3. **예측 (Hypothesis)**: What predictions can you make about future behavior or needs?
   → category: "user_model" or "mutual" depending on whether it's about the user or the relationship

Return a JSON object with a "patterns" array:
{"patterns": [{"content": "pattern (Korean, concise, evidence-based)", "category": "user_model" or "mutual", "importance": 0.8-1.0}, ...]}
If no clear patterns (< 3 supporting facts), return {"patterns": []}.
Return ONLY valid JSON.`

type patternResponse struct {
	Patterns []ExtractedFact `json:"patterns"`
}

func extractPatterns(ctx context.Context, store *Store, embedder *Embedder, client *llm.Client, model string, logger *slog.Logger) (int, error) {
	facts, err := store.GetActiveFacts(ctx)
	if err != nil {
		return 0, err
	}
	logger.Info("aurora-dream: pattern phase input", "active_facts", len(facts))

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
		// P13: Stratified sampling — include both high-importance facts and
		// recent facts so patterns aren't biased toward only well-established
		// knowledge. catFacts are already sorted by importance DESC from
		// GetActiveFacts, so the first N are high-importance.
		const perCatLimit = 20
		const topN = 12
		sampled := stratifiedSample(catFacts, topN, perCatLimit)
		for _, f := range sampled {
			fmt.Fprintf(&sb, "- %s\n", f.Content)
		}
	}

	wrapper, err := callLLMJSON[patternResponse](ctx, client, model, patternSystemPrompt, sb.String(), dreamingMaxTokens)
	if err != nil {
		return 0, nil
	}

	count := 0
	for _, p := range wrapper.Patterns {
		if p.Content == "" {
			continue
		}
		// Accept user_model or mutual category from the LLM; default to user_model.
		cat := CategoryUserModel
		if p.Category == CategoryMutual {
			cat = CategoryMutual
		}
		id, err := store.InsertFact(ctx, Fact{
			Content:    p.Content,
			Category:   cat,
			Importance: clamp(p.Importance, 0.7, 1.0),
			Source:     SourceDreaming,
			MergeDepth: 1, // patterns are already abstractions; prevent cross-depth merging
		})
		if err != nil {
			continue
		}
		count++

		// Embed the pattern so Phase 2 can detect duplicates in future cycles.
		if embedder != nil {
			_ = embedder.EmbedAndStore(ctx, id, p.Content)
		}
	}

	return count, nil
}

// stratifiedSample selects up to total facts from a list, combining topN by
// importance (already sorted by GetActiveFacts) with the remainder from the
// most recently created facts. This ensures pattern extraction sees both
// well-established and fresh facts.
func stratifiedSample(facts []Fact, topN, total int) []Fact {
	if len(facts) <= total {
		return facts
	}
	if topN > total {
		topN = total
	}

	// Take topN by importance (first N, since facts are importance DESC).
	seen := make(map[int64]bool, total)
	result := make([]Fact, 0, total)
	limit := topN
	if limit > len(facts) {
		limit = len(facts)
	}
	for _, f := range facts[:limit] {
		result = append(result, f)
		seen[f.ID] = true
	}

	// Fill remaining slots with most recent facts (by CreatedAt).
	// facts are sorted by importance, so we need to find recent ones.
	remaining := total - len(result)
	if remaining <= 0 {
		return result
	}

	// Collect unseen facts sorted by CreatedAt DESC.
	type recentFact struct {
		fact Fact
		idx  int
	}
	var recent []recentFact
	for i, f := range facts {
		if !seen[f.ID] {
			recent = append(recent, recentFact{f, i})
		}
	}
	sort.Slice(recent, func(i, j int) bool {
		return recent[i].fact.CreatedAt.After(recent[j].fact.CreatedAt)
	})

	for i := 0; i < remaining && i < len(recent); i++ {
		result = append(result, recent[i].fact)
	}

	return result
}

// conflictResult is used by the unified verify-and-resolve response.
type conflictResult struct {
	KeepID   int64  `json:"keep_id"`
	RemoveID int64  `json:"remove_id"`
	Reason   string `json:"reason"`
}

// --- Phase 5: User Model Update ---

type userModelPhase struct{}

func (userModelPhase) Name() string { return "user_model" }
func (userModelPhase) Run(ctx context.Context, s *dreamState) error {
	return updateUserModel(ctx, s.store, s.client, s.model, s.logger)
}

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
	logger.Info("aurora-dream: user_model phase input", "active_facts", len(facts))

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

	profile, err := callLLMJSON[map[string]string](ctx, client, model, userModelSystemPrompt, sb.String(), dreamingMaxTokens)
	if err != nil {
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
