package mailstore

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

const (
	mailSemanticCacheFile            = "semantic-index.json"
	mailSemanticPreprocessingVersion = "mailstore-context-fields-v1"
	mailRRFK                         = 60.0
	mailSemanticRefreshInterval      = 5 * time.Minute
)

// SetEmbedder enables semantic mail retrieval. Corpus vectors are refreshed
// lazily and cached under the mailstore directory; query failures leave the
// existing lexical index fully usable. Options are primarily for deterministic
// tests (WithSyncRefresh); production uses the default background refresh.
func (s *Store) SetEmbedder(embedder embedindex.Embedder, opts ...embedindex.Option) {
	if s == nil {
		return
	}
	if embedder == nil {
		s.mu.Lock()
		previous := s.semantic
		s.semantic = nil
		s.mu.Unlock()
		if previous != nil {
			previous.Close()
		}
		return
	}

	opts = append(opts, embedindex.WithPreprocessingFingerprint(mailSemanticPreprocessingVersion))
	next := embedindex.New("mailstore", embedder, s.semanticCachePath(), opts...)
	s.mu.Lock()
	previous := s.semantic
	s.semantic = next
	s.mu.Unlock()
	if previous != nil {
		previous.Close()
	}
}

func (s *Store) semanticCachePath() string {
	if strings.TrimSpace(s.dir) == "" {
		return ""
	}
	return filepath.Join(s.dir, mailSemanticCacheFile)
}

// semanticItems snapshots the corpus under the store lock, then constructs the
// embeddable text outside it. The embedindex calls this off the query path.
func (s *Store) semanticItems() []embedindex.Item {
	s.mu.RLock()
	messages := make(map[string]mailarchive.ContextMessage, len(s.byKey))
	for key, msg := range s.byKey {
		messages[key] = msg
	}
	s.mu.RUnlock()

	items := make([]embedindex.Item, 0, len(messages))
	for key, msg := range messages {
		text := strings.Join(mailarchive.ContextIndexFields(msg), "\n")
		items = append(items, embedindex.Item{
			ID:   key,
			Hash: embedindex.ContentHash(text),
			Text: text,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (s *Store) semanticSnapshot() *embedindex.Index {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.semantic
}

// WarmSemanticIndex synchronously reconciles the persisted mail corpus. The
// server runs it after the embedding probe becomes healthy so a restart never
// leaves the first user query racing a multi-thousand-message cold build.
func (s *Store) WarmSemanticIndex(ctx context.Context) error {
	semantic := s.semanticSnapshot()
	if semantic == nil {
		return nil
	}
	return semantic.Warm(ctx, s.semanticItems)
}

// SemanticStatus exposes cache freshness without running an embedding probe.
func (s *Store) SemanticStatus() embedindex.Status {
	return s.semanticSnapshot().Status()
}

func (s *Store) SemanticCalibration() embedindex.Calibration {
	return s.semanticSnapshot().Calibration(embedindex.SemanticSurfaceMail)
}

type mailSearchRank struct {
	id            string
	lexicalScore  float64
	semanticScore float64
	lexicalRank   int
	semanticRank  int
	fusedScore    float64
}

// rankedSearch fuses the existing BM25 ranking with dense retrieval. It never
// holds Store.mu across an embedding call. Semantic-only messages must clear a
// conservative cosine floor; BM25 hits remain admitted at every score.
func (s *Store) rankedSearch(ctx context.Context, query string, candidate int) []mailSearchRank {
	s.mu.RLock()
	lexical := s.idx.Search(query, candidate)
	semantic := s.semantic
	s.mu.RUnlock()

	var dense []embedindex.Hit
	if semantic != nil && semantic.Enabled() {
		semantic.RefreshIfStale(s.semanticItems, mailSemanticRefreshInterval)
		dense = semantic.Search(ctx, query, candidate)
	}
	if len(dense) == 0 {
		out := make([]mailSearchRank, 0, len(lexical))
		for rank, hit := range lexical {
			out = append(out, mailSearchRank{id: hit.ID, lexicalScore: hit.Score, lexicalRank: rank + 1, fusedScore: mailRRFScore(rank + 1)})
		}
		return out
	}

	semanticFloor := semantic.Calibration(embedindex.SemanticSurfaceMail).Floor
	byID := make(map[string]*mailSearchRank, len(lexical)+len(dense))
	for rank, hit := range lexical {
		entry := &mailSearchRank{id: hit.ID, lexicalScore: hit.Score, lexicalRank: rank + 1}
		entry.fusedScore += 1 / (mailRRFK + float64(rank+1))
		byID[hit.ID] = entry
	}
	for rank, hit := range dense {
		entry := byID[hit.ID]
		if entry == nil {
			if hit.Score < semanticFloor {
				continue
			}
			entry = &mailSearchRank{id: hit.ID}
			byID[hit.ID] = entry
		}
		entry.semanticScore = hit.Score
		entry.semanticRank = rank + 1
		entry.fusedScore += 1 / (mailRRFK + float64(rank+1))
	}

	// Scale the tiny RRF sum into a readable relevance band: rank 1 in one
	// backend is ~0.4 and agreement at rank 1 is ~0.8.
	scale := 0.4 * (mailRRFK + 1)
	out := make([]mailSearchRank, 0, len(byID))
	for _, entry := range byID {
		entry.fusedScore *= scale
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].fusedScore != out[j].fusedScore {
			return out[i].fusedScore > out[j].fusedScore
		}
		// At equal ranks, preserve exact lexical evidence ahead of semantic-only
		// evidence; then use stable backend ranks and identity.
		if (out[i].lexicalRank > 0) != (out[j].lexicalRank > 0) {
			return out[i].lexicalRank > 0
		}
		if out[i].lexicalRank != out[j].lexicalRank {
			return nonzeroRank(out[i].lexicalRank) < nonzeroRank(out[j].lexicalRank)
		}
		if out[i].semanticRank != out[j].semanticRank {
			return nonzeroRank(out[i].semanticRank) < nonzeroRank(out[j].semanticRank)
		}
		return out[i].id < out[j].id
	})
	return out
}

func mailRRFScore(rank int) float64 {
	return (1 / (mailRRFK + float64(rank))) * (0.4 * (mailRRFK + 1))
}

func nonzeroRank(rank int) int {
	if rank == 0 {
		return int(^uint(0) >> 1)
	}
	return rank
}

func appendMailRankReason(reasons []string, reason string) []string {
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}
