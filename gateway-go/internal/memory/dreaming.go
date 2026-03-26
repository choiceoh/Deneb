// dreaming.go — Periodic memory consolidation inspired by Honcho's "Dreaming" feature.
// Runs every 50 turns or 8 hours to:
//   1. Verify existing facts (still valid?)
//   2. Merge duplicate/similar facts
//   3. Extract meta-patterns from accumulated facts
//   4. Update the user model
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

	logger.Info("dreaming: starting cycle")

	// Phase 1: Fact verification.
	verified, expired, err := verifyFacts(ctx, store, client, model, logger)
	if err != nil {
		logger.Warn("dreaming: verification phase failed", "error", err)
	} else {
		report.FactsVerified = verified
		report.FactsExpired = expired
	}

	// Phase 2: Duplicate merging.
	merged, err := mergeDuplicates(ctx, store, embedder, client, model, logger)
	if err != nil {
		logger.Warn("dreaming: merge phase failed", "error", err)
	} else {
		report.FactsMerged = merged
	}

	// Phase 3: Pattern extraction.
	patterns, err := extractPatterns(ctx, store, client, model, logger)
	if err != nil {
		logger.Warn("dreaming: pattern extraction failed", "error", err)
	} else {
		report.PatternsExtracted = patterns
	}

	// Phase 4: User model update.
	if err := updateUserModel(ctx, store, client, model, logger); err != nil {
		logger.Warn("dreaming: user model update failed", "error", err)
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

	logger.Info("dreaming: cycle complete",
		"verified", report.FactsVerified,
		"merged", report.FactsMerged,
		"expired", report.FactsExpired,
		"patterns", report.PatternsExtracted,
		"duration", report.Duration.Round(time.Second),
	)

	return report, nil
}

// --- Phase 1: Fact Verification ---

const verifySystemPrompt = `You are a memory fact verifier.
Given a list of stored facts (each with an ID), determine which are still valid.
Return a JSON array of objects:
- "id": the fact ID
- "valid": true if still likely valid, false if outdated/incorrect
- "reason": brief reason if invalid (Korean)
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
			logger.Debug("dreaming: verify batch failed", "error", batchErr)
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
		ID    int64  `json:"id"`
		Valid bool   `json:"valid"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(stripCodeFences(resp)), &results); err != nil {
		return 0, 0, fmt.Errorf("parse verify response: %w", err)
	}

	verified, expired := 0, 0
	for _, r := range results {
		if r.Valid {
			_ = store.MarkVerified(ctx, r.ID)
			verified++
		} else {
			_ = store.DeactivateFact(ctx, r.ID)
			expired++
			logger.Info("dreaming: expired fact", "id", r.ID, "reason", r.Reason)
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
		logger.Info("dreaming: merged facts", "old_a", p.a, "old_b", p.b, "new", newID)
	}

	return merged, nil
}

// --- Phase 3: Pattern Extraction ---

const patternSystemPrompt = `You are a pattern recognition assistant.
Given a collection of user facts grouped by category, identify meta-patterns:
- Recurring themes or preferences
- Behavioral patterns
- Expertise areas
Return a JSON array of pattern objects:
- "content": the pattern (Korean, concise)
- "category": "user_model"
- "importance": 0.8-1.0
If no clear patterns, return [].
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

const userModelSystemPrompt = `You are a user profile synthesizer.
Given facts about a user (category: user_model and other categories),
synthesize a structured user profile with these keys:
- communication_style: how the user communicates
- expertise_areas: what the user is expert in
- tech_preferences: preferred technologies and tools
- common_tasks: typical tasks the user performs
- work_patterns: how the user works

Return a JSON object with these keys and Korean values.
If a key cannot be determined, omit it.
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
			logger.Debug("dreaming: failed to set user model", "key", key, "error", err)
		}
	}

	logger.Info("dreaming: updated user model", "keys", len(profile))
	return nil
}

// callLLM is a convenience alias for callSglang (defined in sglang.go).
func callLLM(ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (string, error) {
	return callSglang(ctx, client, model, system, user, maxTokens)
}
