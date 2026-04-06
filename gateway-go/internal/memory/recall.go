// recall.go — FTS-based memory recall with relation chain traversal
// and entity expansion.
//
// The recall engine gathers candidate facts via FTS search, expands them
// through entity links and relation chains, then ranks by score.
package memory

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// RecallConfig controls the recall engine behavior.
type RecallConfig struct {
	Enabled  bool          // enable recall (default true)
	Timeout  time.Duration // recall timeout (default 3s)
	MaxFacts int           // max facts in context pack (default 20)
	MaxDepth int           // max relation chain depth (default 3)
}

// DefaultRecallConfig returns sensible defaults for single-user DGX Spark.
func DefaultRecallConfig() RecallConfig {
	return RecallConfig{
		Enabled:  true,
		Timeout:  3 * time.Second,
		MaxFacts: 20,
		MaxDepth: 3,
	}
}

// Recall performs FTS-based memory recall for the given user message.
// It searches facts, expands via entities and relation chains, then ranks by score.
//
// Returns formatted knowledge text ready for system prompt injection, or ""
// if recall produces no results.
func Recall(ctx context.Context, store *Store, message string, cfg RecallConfig, logger *slog.Logger) string {
	if !cfg.Enabled || store == nil {
		return ""
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	maxFacts := cfg.MaxFacts
	if maxFacts <= 0 {
		maxFacts = 20
	}

	// Phase 1: Gather candidate facts via FTS search.
	candidates, err := store.SearchFacts(ctx, message, SearchOpts{
		Limit:    maxFacts,
		MinScore: 0.35,
	})
	if err != nil || len(candidates) == 0 {
		return ""
	}

	// Phase 2: Expand via entity matching.
	entityNameCache := make(map[int64][]string)
	entityFacts := expandViaEntitiesCached(ctx, store, candidates, maxFacts, entityNameCache)
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

	// Phase 4: Format recall result with timeline and entity summary.
	for _, sr := range candidates {
		if _, ok := entityNameCache[sr.Fact.ID]; !ok {
			entityNameCache[sr.Fact.ID] = store.getFactEntityNames(ctx, sr.Fact.ID)
		}
	}
	return formatRecallKnowledge(candidates, entityNameCache)
}

// formatRecallKnowledge produces the final knowledge text with facts, timeline,
// and entity summary — all generated from code logic (no LLM needed).
func formatRecallKnowledge(candidates []SearchResult, entityNames map[int64][]string) string {
	if len(candidates) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("### 메모리 (recall)\n")

	for _, sr := range candidates {
		date := sr.Fact.CreatedAt.Format("2006-01-02")
		fmt.Fprintf(&sb, "- [%.1f] {%s} (%s) %s\n",
			sr.Fact.Importance, sr.Fact.Category, date, sr.Fact.Content)
	}

	// Timeline: sort by date and show progression.
	if timeline := buildTimeline(candidates); timeline != "" {
		fmt.Fprintf(&sb, "\n### 타임라인\n%s\n", timeline)
	}

	// Entity summary: count entity mentions across facts.
	if entitySummary := buildEntitySummary(candidates, entityNames); entitySummary != "" {
		fmt.Fprintf(&sb, "\n### 엔티티 요약\n%s", entitySummary)
	}

	return sb.String()
}

// buildTimeline creates a chronological progression from candidate facts.
// Groups facts by date and shows the flow of events.
func buildTimeline(candidates []SearchResult) string {
	if len(candidates) < 2 {
		return ""
	}

	// Sort a copy by creation date.
	sorted := make([]SearchResult, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Fact.CreatedAt.Before(sorted[j].Fact.CreatedAt)
	})

	// Build timeline entries: date → truncated content.
	var parts []string
	prevDate := ""
	for _, sr := range sorted {
		date := sr.Fact.CreatedAt.Format("01-02")
		content := truncateContent(sr.Fact.Content, 40)
		entry := date + " " + content
		if date == prevDate {
			// Same date: just show content with arrow.
			entry = content
		}
		prevDate = date
		parts = append(parts, entry)
	}

	return strings.Join(parts, " → ")
}

// buildEntitySummary counts how many facts mention each entity using a pre-populated
// entity name cache. Returns empty string if no entities are linked.
func buildEntitySummary(candidates []SearchResult, entityNames map[int64][]string) string {
	// Count categories per entity across all candidates.
	type entityInfo struct {
		count      int
		categories map[string]bool
	}
	entities := make(map[string]*entityInfo)

	for _, sr := range candidates {
		names := entityNames[sr.Fact.ID]
		for _, name := range names {
			info, ok := entities[name]
			if !ok {
				info = &entityInfo{categories: make(map[string]bool)}
				entities[name] = info
			}
			info.count++
			info.categories[sr.Fact.Category] = true
		}
	}

	if len(entities) == 0 {
		return ""
	}

	// Sort by count descending, limit to top 5.
	type entityEntry struct {
		name string
		info *entityInfo
	}
	sorted := make([]entityEntry, 0, len(entities))
	for name, info := range entities {
		sorted = append(sorted, entityEntry{name, info})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].info.count > sorted[j].info.count
	})
	if len(sorted) > 5 {
		sorted = sorted[:5]
	}

	var sb strings.Builder
	for _, e := range sorted {
		cats := make([]string, 0, len(e.info.categories))
		for c := range e.info.categories {
			cats = append(cats, c)
		}
		sort.Strings(cats)
		fmt.Fprintf(&sb, "- **%s**: %d개 팩트 (%s)\n", e.name, e.info.count, strings.Join(cats, ", "))
	}
	return sb.String()
}

// truncateContent truncates a string to maxLen runes, appending "..." if truncated.
func truncateContent(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// expandViaEntities finds entities linked to existing candidates and adds
// their related facts to the candidate pool.
func expandViaEntities(ctx context.Context, store *Store, existing []SearchResult, maxFacts int) []SearchResult {
	return expandViaEntitiesCached(ctx, store, existing, maxFacts, nil)
}

// expandViaEntitiesCached is like expandViaEntities but populates nameCache
// (fact ID → entity names) as a side effect, avoiding redundant lookups later.
func expandViaEntitiesCached(ctx context.Context, store *Store, existing []SearchResult, maxFacts int, nameCache map[int64][]string) []SearchResult {
	// Extract entity names from existing candidates.
	entityNames := make(map[string]bool)
	for _, sr := range existing {
		names := store.getFactEntityNames(ctx, sr.Fact.ID)
		if nameCache != nil {
			nameCache[sr.Fact.ID] = names
		}
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
