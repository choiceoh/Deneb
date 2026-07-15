// semantic.go — dense-vector recall over RESIDENT sessions' summary nodes.
//
// The keyword search (SearchMessages / SearchResidentSessions) finds past
// messages by literal overlap; it misses a session that is *about* the query but
// phrased it differently. This index embeds each session's DAG summary nodes —
// the compacted, denoised gist of a conversation — and ranks them by cosine, so
// "지난번에 곡성 대금 어떻게 했더라" surfaces the session whose summary says
// "금호 곡성 기성 청구 처리" even with no shared word.
//
// Why summaries, not raw messages: a per-message vector index grows unbounded
// (tens of thousands of messages → 100MB+ of vectors). Summary nodes are few per
// session and already the distilled content, so the index stays small AND the
// recall is denoised. It is bounded to sessions resident in memory this uptime —
// the same scope as SearchResidentSessions — so it does no disk I/O and re-embeds
// only what is loaded. The on-disk cache is intentionally disabled (cachePath
// ""): nothing is resident at boot, so a persisted cache would just be dropped on
// the first refresh; re-embedding the few resident summaries on demand is cheap.
package polaris

import (
	"context"
	"strconv"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
)

// SummaryHit is a semantic match against one session summary node.
type SummaryHit struct {
	SessionKey string
	Content    string
	CreatedAt  int64
	Score      float64 // cosine (0–1)
}

// SetSummaryEmbedder wires (or clears, on nil embedder) the summary semantic
// index. The vector cache is disabled by design (see file header). Idempotent.
func (s *Store) SetSummaryEmbedder(e embedindex.Embedder) {
	if s.summarySem != nil {
		s.summarySem.Close()
		s.summarySem = nil
	}
	if e == nil {
		return
	}
	s.summarySem = embedindex.New("session", e, "" /* no on-disk cache */, embedindex.WithPreprocessingFingerprint("session-summary-v1"))
}

// closeSummarySem stops the summary semantic index (folded into Store.Close).
func (s *Store) closeSummarySem() {
	if s != nil && s.summarySem != nil {
		s.summarySem.Close()
	}
}

// warmSummarySem synchronously embeds resident sessions' summaries. Not called at
// startup (nothing is resident at boot — the index fills lazily as sessions load
// and are searched); used by tests for a deterministic first search.
func (s *Store) warmSummarySem(ctx context.Context) error {
	if s == nil || s.summarySem == nil {
		return nil
	}
	return s.summarySem.Warm(ctx, s.summaryItems)
}

// summaryItemID keys a summary node's vector by session + node id (NUL separator,
// which cannot appear in a session key).
func summaryItemID(sessionKey string, nodeID int64) string {
	return sessionKey + "\x00" + strconv.FormatInt(nodeID, 10)
}

// summaryItems enumerates the summary nodes of every resident session for
// embedding. Called on each refresh (off the query path), under the store lock.
func (s *Store) summaryItems() []embedindex.Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []embedindex.Item
	for key, sd := range s.sessions {
		for _, n := range sd.summaries {
			if n.Content == "" {
				continue
			}
			items = append(items, embedindex.Item{
				ID:   summaryItemID(key, n.ID),
				Hash: embedindex.ContentHash(n.Content),
				Text: n.Content,
			})
		}
	}
	return items
}

// SearchSummariesSemantic returns per-query semantic matches against resident
// session summaries, index-aligned with queries and excluding excludeKey (the
// current session's summaries are already in context). Kicks a background
// re-embed first, then scans current vectors. nil when semantic is disabled, so
// the caller keeps the keyword cross-session search.
func (s *Store) SearchSummariesSemantic(ctx context.Context, excludeKey string, queries []string, limit int) [][]SummaryHit {
	if s.summarySem == nil || !s.summarySem.Enabled() {
		return nil
	}
	s.summarySem.RefreshAsync(s.summaryItems)
	batch := s.summarySem.SearchBatch(ctx, queries, limit)
	if batch == nil {
		return nil
	}

	// Snapshot id → summary for the resident sessions (minus the excluded one),
	// so a returned vector id maps back to its node. Taken in its own critical
	// section — never while calling the index (whose refresh takes s.mu too).
	lookup := make(map[string]SummaryHit)
	s.mu.Lock()
	for key, sd := range s.sessions {
		if key == excludeKey {
			continue
		}
		for _, n := range sd.summaries {
			lookup[summaryItemID(key, n.ID)] = SummaryHit{
				SessionKey: key,
				Content:    n.Content,
				CreatedAt:  n.CreatedAt,
			}
		}
	}
	s.mu.Unlock()

	out := make([][]SummaryHit, len(batch))
	for i, hits := range batch {
		for _, h := range hits {
			sh, ok := lookup[h.ID]
			if !ok {
				continue // excluded session or a node dropped since the embed
			}
			sh.Score = h.Score
			out[i] = append(out[i], sh)
		}
	}
	return out
}
