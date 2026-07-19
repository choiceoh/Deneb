package workfeed

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
)

const (
	workFeedSemanticPreprocessingVersion = "workfeed-title-summary-body-v1"
	// 0.47 keeps cross-project same-topic hard negatives below the boundary
	// while recovering paraphrased cards measured with the production embedder.
	workFeedSemanticGroupFloor     = 0.47
	workFeedSemanticCandidateLimit = 200
	workFeedSemanticMaxAge         = 45 * 24 * time.Hour
	workFeedSemanticMaxRelated     = 20
	workFeedSemanticMaxRunes       = 4000
)

func workFeedSemanticMatches(ctx context.Context, semantic *embedindex.Index, item Item, existing []Item) []string {
	query := workFeedSemanticText(item)
	if semantic == nil || utf8.RuneCountInString(query) < 8 {
		return nil
	}
	candidates := workFeedSemanticCandidates(existing, time.Now())
	if len(candidates) == 0 {
		return nil
	}
	if err := semantic.Warm(ctx, func() []embedindex.Item {
		out := make([]embedindex.Item, 0, len(candidates))
		for _, candidate := range candidates {
			text := workFeedSemanticText(candidate)
			out = append(out, embedindex.Item{ID: candidate.ID, Hash: embedindex.ContentHash(text), Text: text})
		}
		return out
	}); err != nil {
		return nil
	}
	hits := semantic.Search(ctx, query, 8)
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit.Score < workFeedSemanticGroupFloor {
			continue
		}
		out = append(out, hit.ID)
	}
	return out
}

func workFeedSemanticCandidates(items []Item, now time.Time) []Item {
	cutoff := now.Add(-workFeedSemanticMaxAge).UnixMilli()
	out := make([]Item, 0, min(len(items), workFeedSemanticCandidateLimit))
	for i := len(items) - 1; i >= 0; i-- {
		item := normalizeExisting(items[i])
		if item.Status == StatusAcked || (item.CreatedAtMs > 0 && item.CreatedAtMs < cutoff) || utf8.RuneCountInString(workFeedSemanticText(item)) < 8 {
			continue
		}
		out = append(out, item)
		if len(out) >= workFeedSemanticCandidateLimit {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAtMs != out[j].CreatedAtMs {
			return out[i].CreatedAtMs > out[j].CreatedAtMs
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func workFeedSemanticText(item Item) string {
	text := strings.TrimSpace(item.Title + "\n" + item.Summary + "\n" + item.Body)
	runes := []rune(text)
	if len(runes) > workFeedSemanticMaxRunes {
		text = string(runes[:workFeedSemanticMaxRunes])
	}
	return text
}

// applySemanticGroup merges advisory cluster metadata onto related retained
// cards and the new card. It never removes or settles a card.
func applySemanticGroup(items []Item, item Item, relatedIDs []string) Item {
	if len(relatedIDs) == 0 {
		return item
	}
	hitIDs := make(map[string]struct{}, len(relatedIDs))
	for _, id := range relatedIDs {
		hitIDs[id] = struct{}{}
	}
	clusterIDs := map[string]struct{}{}
	memberIDs := map[string]struct{}{item.ID: {}}
	for i := range items {
		if _, hit := hitIDs[items[i].ID]; !hit {
			continue
		}
		memberIDs[items[i].ID] = struct{}{}
		if items[i].ClusterID != "" {
			clusterIDs[items[i].ClusterID] = struct{}{}
		}
	}
	if len(memberIDs) < 2 {
		return item
	}
	// Merge complete pre-existing clusters touched by the hits.
	for i := range items {
		if _, merge := clusterIDs[items[i].ClusterID]; merge && items[i].ClusterID != "" {
			memberIDs[items[i].ID] = struct{}{}
		}
	}
	clusterID := canonicalWorkFeedClusterID(clusterIDs, memberIDs)
	members := make([]string, 0, len(memberIDs))
	for id := range memberIDs {
		members = append(members, id)
	}
	sort.Strings(members)
	for i := range items {
		if _, member := memberIDs[items[i].ID]; !member {
			continue
		}
		items[i].ClusterID = clusterID
		items[i].RelatedIDs = relatedWorkFeedIDs(members, items[i].ID)
	}
	item.ClusterID = clusterID
	item.RelatedIDs = relatedWorkFeedIDs(members, item.ID)
	return item
}

func canonicalWorkFeedClusterID(clusterIDs, memberIDs map[string]struct{}) string {
	ids := make([]string, 0, len(clusterIDs))
	for id := range clusterIDs {
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		sort.Strings(ids)
		return ids[0]
	}
	for id := range memberIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return "wfc_" + embedindex.ContentHash(ids[0])
}

func relatedWorkFeedIDs(members []string, self string) []string {
	out := make([]string, 0, min(len(members)-1, workFeedSemanticMaxRelated))
	for _, id := range members {
		if id == self {
			continue
		}
		out = append(out, id)
		if len(out) >= workFeedSemanticMaxRelated {
			break
		}
	}
	return out
}
