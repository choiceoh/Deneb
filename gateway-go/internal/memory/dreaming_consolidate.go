// dreaming_consolidate.go — Cluster-based text consolidation phase for Aurora Dreaming.
// Runs after pairwise merge to catch semantic paraphrases that Jaccard misses.
// Groups facts into clusters by Jaccard text similarity, then asks the LLM
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
	// Jaccard threshold for clustering (lower than merge threshold).
	consolidateJaccardThreshold = 0.45

	// Only consolidate clusters with 3+ facts (pairs are handled by merge phase).
	consolidateMinClusterSize = 3

	// Cap LLM calls per cycle.
	consolidateMaxClusters = 8

	// Max facts sent to LLM per cluster.
	consolidateMaxClusterFacts = 40

	// Cap per-category fact load.
	consolidateMaxPerCategory = 200
)

type consolidatePhase struct{}

func (consolidatePhase) Name() string { return "consolidate" }

func (consolidatePhase) Run(ctx context.Context, s *dreamState) error {
	consolidated, err := consolidateClusters(ctx, s.store, s.client, s.model, s.logger)
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

func consolidateClusters(ctx context.Context, store *Store, client *llm.Client, model string, logger *slog.Logger) (int, error) {
	facts, err := store.GetActiveFacts(ctx)
	if err != nil {
		return 0, err
	}

	// Group by category.
	catFacts := map[string][]Fact{}
	for _, f := range facts {
		catFacts[f.Category] = append(catFacts[f.Category], f)
	}

	// Cap per category (keep most important — GetActiveFacts returns sorted by importance DESC).
	for cat, fs := range catFacts {
		if len(fs) > consolidateMaxPerCategory {
			catFacts[cat] = fs[:consolidateMaxPerCategory]
		}
	}

	// Build clusters using union-find within each category via Jaccard similarity.
	type factEntry struct {
		fact Fact
		idx  int // position in flat list
	}
	var allFacts []factEntry
	for _, fs := range catFacts {
		for _, f := range fs {
			allFacts = append(allFacts, factEntry{fact: f, idx: len(allFacts)})
		}
	}

	parent := make([]int, len(allFacts))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	// Connect pairs above threshold within same category.
	catStart := 0
	for _, fs := range catFacts {
		catEnd := catStart + len(fs)
		for i := catStart; i < catEnd; i++ {
			for j := i + 1; j < catEnd; j++ {
				sim := JaccardTextSimilarity(allFacts[i].fact.Content, allFacts[j].fact.Content)
				if sim >= consolidateJaccardThreshold {
					union(i, j)
				}
			}
		}
		catStart = catEnd
	}

	// Collect clusters.
	clusterMap := map[int][]int{}
	for i := range allFacts {
		root := find(i)
		clusterMap[root] = append(clusterMap[root], i)
	}

	// Filter to clusters above minimum size, sort by size descending.
	type cluster struct {
		indices []int
	}
	var clusters []cluster
	for _, indices := range clusterMap {
		if len(indices) >= consolidateMinClusterSize {
			sort.Ints(indices)
			clusters = append(clusters, cluster{indices: indices})
		}
	}
	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i].indices) > len(clusters[j].indices)
	})

	if len(clusters) == 0 {
		return 0, nil
	}
	if len(clusters) > consolidateMaxClusters {
		clusters = clusters[:consolidateMaxClusters]
	}

	logger.Info("aurora-dream: consolidate phase",
		"clusters", len(clusters),
		"largest", len(clusters[0].indices),
	)

	totalConsolidated := 0

	for ci, cl := range clusters {
		indices := cl.indices
		if len(indices) > consolidateMaxClusterFacts {
			indices = indices[:consolidateMaxClusterFacts]
		}

		// Load fact contents.
		var lines []string
		var activeFacts []Fact
		for _, idx := range indices {
			f := allFacts[idx].fact
			if !f.Active {
				continue
			}
			activeFacts = append(activeFacts, f)
			lines = append(lines, fmt.Sprintf("[#%d] {%s} %.2f: %s", f.ID, f.Category, f.Importance, f.Content))
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
