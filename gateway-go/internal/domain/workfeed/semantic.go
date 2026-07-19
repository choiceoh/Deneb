package workfeed

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	workFeedSemanticPreprocessingVersion = "workfeed-title-summary-body-v1"
	workFeedSemanticCandidateLimit       = 200
	workFeedSemanticMaxAge               = 45 * 24 * time.Hour
	workFeedSemanticMaxRelated           = 20
	workFeedSemanticMaxCluster           = 8
	workFeedSemanticMaxRunes             = 4000
)

// WarmSemanticIndex prepares retained active cards before the first append, so
// grouping does not pay a cold corpus embed on the append path.
func (s *Store) WarmSemanticIndex(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	semantic := s.semantic
	items, err := jsonlstore.Load[Item](s.path)
	s.mu.Unlock()
	if err != nil || semantic == nil {
		return err
	}
	candidates := workFeedSemanticCandidates(items, time.Now())
	return semantic.Warm(ctx, func() []embedindex.Item {
		return workFeedSemanticIndexItems(candidates)
	})
}

// SemanticStatus exposes cache freshness without issuing a sidecar request.
func (s *Store) SemanticStatus() embedindex.Status {
	if s == nil {
		return embedindex.Status{}
	}
	s.mu.Lock()
	semantic := s.semantic
	s.mu.Unlock()
	return semantic.Status()
}

func (s *Store) SemanticCalibration() embedindex.Calibration {
	if s == nil {
		return embedindex.CalibrationFor(nil, embedindex.SemanticSurfaceWorkFeed)
	}
	s.mu.Lock()
	semantic := s.semantic
	s.mu.Unlock()
	return semantic.Calibration(embedindex.SemanticSurfaceWorkFeed)
}

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
		return workFeedSemanticIndexItems(candidates)
	}); err != nil {
		return nil
	}
	hits := semantic.Search(ctx, query, 8)
	semanticFloor := semantic.Calibration(embedindex.SemanticSurfaceWorkFeed).Floor
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit.Score < semanticFloor {
			continue
		}
		out = append(out, hit.ID)
	}
	return out
}

func workFeedSemanticIndexItems(candidates []Item) []embedindex.Item {
	out := make([]embedindex.Item, 0, len(candidates))
	for _, candidate := range candidates {
		text := workFeedSemanticText(candidate)
		out = append(out, embedindex.Item{ID: candidate.ID, Hash: embedindex.ContentHash(text), Text: text})
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

// applySemanticGroup attaches a new card to at most one existing cluster. The
// strongest semantic hit selects the cluster; other pre-existing clusters are
// never merged through a weak transitive bridge, and cluster growth is capped.
func applySemanticGroup(items []Item, item Item, relatedIDs []string) Item {
	if len(relatedIDs) == 0 {
		return item
	}
	byID := make(map[string]int, len(items))
	for i := range items {
		if items[i].Status != StatusAcked {
			byID[items[i].ID] = i
		}
	}
	bestIndex := -1
	for _, id := range relatedIDs {
		if index, ok := byID[id]; ok {
			bestIndex = index
			break
		}
	}
	if bestIndex < 0 {
		return item
	}
	targetCluster := strings.TrimSpace(items[bestIndex].ClusterID)
	memberIDs := map[string]struct{}{item.ID: {}}
	if targetCluster != "" {
		for i := range items {
			if items[i].Status != StatusAcked && items[i].ClusterID == targetCluster {
				memberIDs[items[i].ID] = struct{}{}
			}
		}
		if len(memberIDs) > workFeedSemanticMaxCluster {
			return item
		}
	} else {
		for _, id := range relatedIDs {
			index, ok := byID[id]
			if !ok || strings.TrimSpace(items[index].ClusterID) != "" {
				continue
			}
			memberIDs[id] = struct{}{}
			if len(memberIDs) >= workFeedSemanticMaxCluster {
				break
			}
		}
	}
	if len(memberIDs) < 2 {
		return item
	}
	clusterID := canonicalWorkFeedClusterID(targetCluster, memberIDs)
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

func canonicalWorkFeedClusterID(existing string, memberIDs map[string]struct{}) string {
	if existing = strings.TrimSpace(existing); existing != "" {
		return existing
	}
	ids := make([]string, 0, len(memberIDs))
	for id := range memberIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return "wfc_" + embedindex.ContentHash(ids[0])
}

// reconcileSemanticGroups removes acknowledged/deleted members and rebuilds
// symmetric RelatedIDs from the surviving cluster membership. It repairs old
// one-way/stale metadata opportunistically whenever the feed is read or saved.
func reconcileSemanticGroups(items []Item) []Item {
	clusters := make(map[string]map[string]struct{})
	for i := range items {
		clusterID := strings.TrimSpace(items[i].ClusterID)
		if items[i].Status == StatusAcked || clusterID == "" || strings.TrimSpace(items[i].ID) == "" {
			continue
		}
		members := clusters[clusterID]
		if members == nil {
			members = make(map[string]struct{})
			clusters[clusterID] = members
		}
		members[items[i].ID] = struct{}{}
	}
	for i := range items {
		clusterID := strings.TrimSpace(items[i].ClusterID)
		members := clusters[clusterID]
		if items[i].Status == StatusAcked || clusterID == "" || len(members) < 2 {
			items[i].ClusterID = ""
			items[i].RelatedIDs = nil
			continue
		}
		ids := make([]string, 0, len(members))
		for id := range members {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		items[i].RelatedIDs = relatedWorkFeedIDs(ids, items[i].ID)
	}
	return items
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
