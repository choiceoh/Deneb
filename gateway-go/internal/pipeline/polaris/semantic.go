// semantic.go — dense-vector recall over RESIDENT sessions' structured
// conversation artifacts and admitted high-signal bursts.
//
// The keyword search (SearchMessages / SearchResidentSessions) finds past
// messages by literal overlap; it misses a session that is *about* the query but
// phrased it differently. This index embeds each session's DAG summary nodes —
// the compacted, denoised gist of a conversation — and ranks them by cosine, so
// "지난번에 곡성 대금 어떻게 했더라" surfaces the session whose summary says
// "금호 곡성 기성 청구 처리" even with no shared word.
//
// Why artifacts/bursts, not every raw message: a per-message vector index grows
// unbounded and embeds low-signal chatter. Raw messages stay losslessly FTS
// searchable; one normalized artifact per summary range plus a small, gated set
// of consecutive high-signal bursts supplies semantic recall. It is bounded to
// sessions resident in memory this uptime —
// the same scope as SearchResidentSessions — so it does no disk I/O and re-embeds
// only what is loaded. The on-disk cache is intentionally disabled (cachePath
// ""): nothing is resident at boot, so a persisted cache would just be dropped on
// the first refresh; re-embedding the few resident summaries on demand is cheap.
package polaris

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
)

// SummaryHit is a semantic match against one session summary node.
type SummaryHit struct {
	SessionKey     string
	Content        string
	CreatedAt      int64
	Score          float64 // cosine (0–1)
	Representation string  // artifact | burst | summary-fallback
	Artifact       *ConversationArtifact
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
	s.summarySem = embedindex.New("session", e, "" /* no on-disk cache */, embedindex.WithPreprocessingFingerprint("session-artifact-burst-v2"))
}

// closeSummarySem stops the summary semantic index (folded into Store.Close).
func (s *Store) closeSummarySem() {
	if s != nil && s.summarySem != nil {
		s.summarySem.Close()
	}
}

// WarmSemanticIndex synchronously embeds the resident sessions' semantic nodes.
// Polaris was the only semantic index with no warm target registered beside
// mail/workfeed/wiki, so its first search after a session loads returned nothing
// while RefreshAsync was still filling. Named to match the other stores so the
// server's warmer list reads uniformly.
func (s *Store) WarmSemanticIndex(ctx context.Context) error {
	return s.warmSummarySem(ctx)
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

// semanticItemID keys one artifact/burst vector by session + summary node +
// representation (NUL cannot appear in a session key).
func semanticItemID(sessionKey string, nodeID int64, representation string, index int) string {
	return sessionKey + "\x00" + strconv.FormatInt(nodeID, 10) + "\x00" + representation + "\x00" + strconv.Itoa(index)
}

type semanticRepresentation struct {
	id             string
	text           string
	content        string
	representation string
	artifact       *ConversationArtifact
}

func nodeSemanticRepresentations(sessionKey string, node SummaryNode) []semanticRepresentation {
	artifact := node.Artifact
	if artifact == nil {
		text := strings.TrimSpace(node.Content)
		if text == "" {
			return nil
		}
		return []semanticRepresentation{{
			id: semanticItemID(sessionKey, node.ID, "summary", 0), text: text, content: text,
			representation: "summary-fallback",
		}}
	}
	var out []semanticRepresentation
	if text := artifact.embeddingText(); text != "" {
		out = append(out, semanticRepresentation{
			id: semanticItemID(sessionKey, node.ID, "artifact", 0), text: text, content: text,
			representation: "artifact", artifact: artifact,
		})
	}
	for i, burst := range artifact.Bursts {
		text := artifact.burstEmbeddingText(burst)
		if text == "" {
			continue
		}
		out = append(out, semanticRepresentation{
			id: semanticItemID(sessionKey, node.ID, "burst", i), text: text, content: text,
			representation: "burst", artifact: artifact,
		})
	}
	return out
}

// summaryItems enumerates the summary nodes of every resident session for
// embedding. Called on each refresh (off the query path), under the store lock.
func (s *Store) summaryItems() []embedindex.Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []embedindex.Item
	// Deterministic key order: the index and its callers tie-break on arrival
	// order, so map iteration would make identical corpora rank differently
	// between runs.
	keys := make([]string, 0, len(s.sessions))
	for key := range s.sessions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sd := s.sessions[key]
		for _, n := range sd.semanticNodes() {
			for _, representation := range nodeSemanticRepresentations(key, n) {
				items = append(items, embedindex.Item{
					ID: representation.id, Hash: embedindex.ContentHash(representation.text), Text: representation.text,
				})
			}
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
		for _, n := range sd.semanticNodes() {
			for _, representation := range nodeSemanticRepresentations(key, n) {
				lookup[representation.id] = SummaryHit{
					SessionKey: key, Content: representation.content, CreatedAt: n.CreatedAt,
					Representation: representation.representation, Artifact: representation.artifact,
				}
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

// unsummarizedTailNodeID is the synthetic node id for a session's tail
// representations. Real summary ids come from nextSumID and start at 1, so a
// negative id cannot collide.
const unsummarizedTailNodeID = -1

// unsummarizedTailNode synthesizes a summary node over the messages no summary
// covers yet, so they get the SAME deterministic burst treatment real summaries
// get. Without it, semantic recall reaches only what DAG compaction has already
// condensed — a session that has not been compacted (a young one, or every
// session in a fresh store) is semantically invisible no matter how much it
// contains, and cross-session recall silently falls back to keyword overlap.
//
// This is not the per-message vector index the file header rejects. The bounds
// are exactly the ones the artifact design already accepted: artifactBursts
// admits only consecutive same-role runs scoring >= 2 and caps the count at
// artifactMaxBursts, so a chatty tail contributes a handful of vectors, not one
// per message. Content (not the id) carries the hash, so a growing tail
// re-embeds only the burst it actually changed.
func (sd *sessionData) unsummarizedTailNode() (SummaryNode, bool) {
	if sd == nil || len(sd.messages) == 0 {
		return SummaryNode{}, false
	}
	start := 0
	for _, n := range sd.summaries {
		if n.MsgEnd+1 > start {
			start = n.MsgEnd + 1
		}
	}
	tail := make([]messageRecord, 0, len(sd.messages))
	for _, m := range sd.messages {
		if m.MsgIndex >= start {
			tail = append(tail, m)
		}
	}
	if len(tail) == 0 {
		return SummaryNode{}, false
	}
	node := SummaryNode{
		ID:       unsummarizedTailNodeID,
		Level:    1,
		MsgStart: tail[0].MsgIndex,
		MsgEnd:   tail[len(tail)-1].MsgIndex,
	}
	// Content stays empty: there is no summary text for an uncompacted span, and
	// deriveConversationArtifact must not invent one. Question and Bursts come
	// from the raw messages, which is the whole point.
	node.Artifact = deriveConversationArtifact(node, tail)
	if node.Artifact == nil || len(node.Artifact.Bursts) == 0 {
		// No admitted burst means nothing cleared the signal gate. Indexing the
		// bare artifact then would embed exactly the low-signal chatter the
		// design excludes.
		return SummaryNode{}, false
	}
	return node, true
}

// semanticNodes returns the summary nodes a session contributes to the semantic
// index: its real summaries plus the synthetic uncompacted tail.
func (sd *sessionData) semanticNodes() []SummaryNode {
	nodes := sd.summaries
	if tail, ok := sd.unsummarizedTailNode(); ok {
		nodes = append(append([]SummaryNode(nil), nodes...), tail)
	}
	return nodes
}
