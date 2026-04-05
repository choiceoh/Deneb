// dreaming_consolidate.go — Cluster-based semantic consolidation phase for Aurora Dreaming.
// Runs after pairwise merge to catch semantic paraphrases that cosine ≥0.78 misses.
// Groups facts into clusters at a lower similarity threshold, then asks the LLM
// to consolidate each cluster into 1-2 canonical facts.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Consolidation tuning constants.
const (
	// Lower than merge threshold (0.78) to catch paraphrases.
	consolidateSimThreshold = 0.62

	// Only consolidate clusters with 3+ facts (pairs are handled by merge phase).
	consolidateMinClusterSize = 3

	// Cap LLM calls per cycle.
	consolidateMaxClusters = 8

	// Max facts sent to LLM per cluster.
	consolidateMaxClusterFacts = 40

	// Cap per-category embedding load (higher than merge's 100).
	consolidateMaxPerCategory = 200
)

type consolidatePhase struct{}

func (consolidatePhase) Name() string { return "consolidate" }

func (consolidatePhase) Run(ctx context.Context, s *dreamState) error {
	// Clustering uses existing DB embeddings (no embedder required).
	// Embedder is only needed to embed new consolidated facts (optional).
	consolidated, err := consolidateClusters(ctx, s.store, s.embedder, s.client, s.model, s.logger)
	if err != nil {
		return err
	}
	// Consolidation count rolls into FactsMerged (same semantic operation).
	s.report.FactsMerged += consolidated
	return nil
}

const consolidateSystemPrompt = `You are a memory consolidation assistant.
Given a cluster of facts that express similar or overlapping information,
consolidate them into 1-2 canonical facts that preserve ALL unique information.
Drop redundant restatements but keep distinct details.

Return a JSON object:
- "facts": array of consolidated facts, each with:
  - "content": consolidated fact text (Korean, concise, complete)
  - "category": best category (context/preference/decision/solution/user_model/mutual)
  - "importance": importance score 0.0-1.0
Return ONLY valid JSON, no markdown fences.`

type consolidateResponse struct {
	Facts []struct {
		Content    string  `json:"content"`
		Category   string  `json:"category"`
		Importance float64 `json:"importance"`
	} `json:"facts"`
}

func consolidateClusters(ctx context.Context, store *Store, embedder *Embedder, client *llm.Client, model string, logger *slog.Logger) (int, error) {
	// Load all embeddings (no depth restriction — semantic dupes span depths).
	embeddings, _, categories, err := store.LoadEmbeddingsForMerge(ctx, 99)
	if err != nil {
		return 0, err
	}

	// Group by category.
	catGroups := map[string][]int64{}
	for id := range embeddings {
		cat := categories[id]
		catGroups[cat] = append(catGroups[cat], id)
	}

	// Cap per category.
	for cat, ids := range catGroups {
		if len(ids) > consolidateMaxPerCategory {
			// Keep highest IDs (most recent).
			sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
			catGroups[cat] = ids[:consolidateMaxPerCategory]
		}
	}

	// Build clusters using union-find within each category.
	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if p, ok := parent[x]; ok && p != x {
			parent[x] = find(p)
			return parent[x]
		}
		return x
	}
	union := func(a, b int64) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	// Initialize union-find.
	for _, ids := range catGroups {
		for _, id := range ids {
			parent[id] = id
		}
	}

	// Connect pairs above threshold within same category.
	for _, ids := range catGroups {
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				sim := cosineSimilarity(embeddings[ids[i]], embeddings[ids[j]])
				if sim >= consolidateSimThreshold {
					union(ids[i], ids[j])
				}
			}
		}
	}

	// Collect clusters.
	clusterMap := map[int64][]int64{}
	for id := range parent {
		root := find(id)
		clusterMap[root] = append(clusterMap[root], id)
	}

	// Filter to clusters above minimum size, sort by size descending.
	type cluster struct {
		ids []int64
	}
	var clusters []cluster
	for _, ids := range clusterMap {
		if len(ids) >= consolidateMinClusterSize {
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			clusters = append(clusters, cluster{ids: ids})
		}
	}
	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i].ids) > len(clusters[j].ids)
	})

	if len(clusters) == 0 {
		return 0, nil
	}
	if len(clusters) > consolidateMaxClusters {
		clusters = clusters[:consolidateMaxClusters]
	}

	logger.Info("aurora-dream: consolidate phase",
		"clusters", len(clusters),
		"largest", len(clusters[0].ids),
	)

	totalConsolidated := 0

	for ci, cl := range clusters {
		ids := cl.ids
		if len(ids) > consolidateMaxClusterFacts {
			ids = ids[:consolidateMaxClusterFacts]
		}

		// Load fact contents.
		var lines []string
		var activeFacts []Fact
		for _, id := range ids {
			fact, err := store.GetFactReadOnly(ctx, id)
			if err != nil || !fact.Active {
				continue
			}
			activeFacts = append(activeFacts, *fact)
			lines = append(lines, fmt.Sprintf("[#%d] {%s} %.2f: %s", fact.ID, fact.Category, fact.Importance, fact.Content))
		}
		if len(activeFacts) < consolidateMinClusterSize {
			continue
		}

		prompt := fmt.Sprintf("Cluster of %d similar facts:\n%s", len(lines), strings.Join(lines, "\n"))
		result, err := callLLMJSON[consolidateResponse](ctx, client, model, consolidateSystemPrompt, prompt, 2048)
		if err != nil {
			logger.Warn("aurora-dream: consolidate LLM failed", "cluster", ci, "error", err)
			continue
		}
		if len(result.Facts) == 0 {
			continue
		}

		// Insert consolidated facts and supersede originals.
		var newIDs []int64
		var pendingEmbeds []struct {
			ID      int64
			Content string
		}

		for _, cf := range result.Facts {
			if cf.Content == "" {
				continue
			}
			cat := cf.Category
			if !isValidCategory(cat) {
				cat = activeFacts[0].Category
			}
			newID, err := store.InsertFact(ctx, Fact{
				Content:    cf.Content,
				Category:   cat,
				Importance: clamp(cf.Importance, 0, 1),
				Source:     SourceDreaming,
				MergeDepth: maxConsolidateDepth(activeFacts),
			})
			if err != nil {
				continue
			}
			newIDs = append(newIDs, newID)
			pendingEmbeds = append(pendingEmbeds, struct {
				ID      int64
				Content string
			}{ID: newID, Content: cf.Content})
		}

		if len(newIDs) == 0 {
			continue
		}

		// Supersede all original facts.
		supersededCount := 0
		for _, fact := range activeFacts {
			if err := store.SupersedeFact(ctx, fact.ID, newIDs[0]); err == nil {
				supersededCount++
			}
		}

		// Batch-embed consolidated facts (skip if no embedder).
		if embedder != nil && len(pendingEmbeds) > 0 {
			if n, err := embedder.EmbedBatchAndStore(ctx, pendingEmbeds); err != nil {
				logger.Warn("aurora-dream: consolidate embed failed", "error", err)
			} else {
				logger.Info("aurora-dream: consolidate embedded", "count", n)
			}
		}

		totalConsolidated += supersededCount
		logger.Info("aurora-dream: consolidated cluster",
			"cluster", ci,
			"original", len(activeFacts),
			"consolidated_to", len(newIDs),
			"superseded", supersededCount,
		)
	}

	return totalConsolidated, nil
}

// maxConsolidateDepth returns max(merge_depth) + 1 for the consolidated fact.
func maxConsolidateDepth(facts []Fact) int {
	maxD := 0
	for _, f := range facts {
		if f.MergeDepth > maxD {
			maxD = f.MergeDepth
		}
	}
	return maxD + 1
}
