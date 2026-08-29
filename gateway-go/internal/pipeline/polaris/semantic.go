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
	"os"
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

// WarmSemanticIndex hydrates the most recently active sessions, then embeds
// their semantic nodes.
//
// The hydration step is the point. This warmer runs at STARTUP, when the
// resident set is empty, so warming it alone embedded nothing and still logged
// "semantic index warmed" — the index only ever filled with whatever sessions
// the process later happened to touch. That left the two cross-session arms
// asymmetric in a way that silently defeats the semantic one: the keyword arm
// searches every transcript on disk (FileTranscriptStore.Search walks
// ListKeys), while the semantic arm saw only resident sessions. A question
// whose instances share no keyword — the exact shape the semantic arm exists
// for — was therefore invisible after every restart.
//
// Bounded by recency rather than unbounded: warmSessionHydrationLimit caps how
// many sessions are pulled in, so a long-lived corpus cannot grow the resident
// set (or the embedding bill on each restart, since the session index keeps no
// on-disk vector cache) without limit.
func (s *Store) WarmSemanticIndex(ctx context.Context) error {
	hydrated := s.hydrateRecentSessions(warmSessionHydrationLimit())
	err := s.warmSummarySem(ctx)
	// Report the sizes: "semantic index warmed" on its own cannot tell a full
	// index from an empty one, which is exactly how this warm reported success
	// while embedding nothing.
	s.mu.Lock()
	logger, resident := s.logger, len(s.sessions)
	s.mu.Unlock()
	if logger != nil {
		logger.Info("polaris: semantic warm",
			"hydrated", hydrated, "residentSessions", resident, "vectors", s.semanticItemCount())
	}
	return err
}

// semanticItemCount reports how many vectors the summary index would hold for
// the resident set — the number that distinguishes a warmed index from a warm
// that found nothing to embed.
func (s *Store) semanticItemCount() int {
	if s == nil || s.summarySem == nil {
		return 0
	}
	return len(s.summaryItems())
}

// warmSessionHydrationLimit is how many recently-active sessions warm-time
// hydration pulls in. Override with DENEB_POLARIS_WARM_SESSIONS (0 disables
// hydration and restores the resident-only behavior).
//
// Hydrated sessions stay resident for the process lifetime, so this cap is a
// standing memory cost, not a startup one. Measured against the operator's
// corpus (325 sessions, 40MB of transcripts), heap and indexed vectors do not
// grow together:
//
//	limit    heap     vectors
//	20        3 MB        89
//	40       17 MB       189
//	60       28 MB       298
//	80       36 MB       362   <- default
//	120     187 MB      1136
//	320     312 MB      2288
//
// The knee is real: a handful of very large transcripts (a long-running dream
// session and friends) sit just past 80, so raising the cap to 120 costs 5x
// the memory for 3x the vectors. 80 buys most of the reachable corpus at a
// size that cannot push this host into the earlyoom territory that has killed
// the embedding sidecar before.
func warmSessionHydrationLimit() int {
	if raw := strings.TrimSpace(os.Getenv("DENEB_POLARIS_WARM_SESSIONS")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			return n
		}
	}
	return 80
}

// hydrateRecentSessions makes the `limit` most recently active sessions
// resident so the semantic warm has something to embed. Ordering is by the
// message file's modification time, newest first: recall reaches for recent
// conversations far more often than old ones, and the cap has to spend itself
// somewhere. Returns how many sessions it added.
func (s *Store) hydrateRecentSessions(limit int) int {
	if s == nil || limit <= 0 {
		return 0
	}
	s.mu.Lock()
	list := s.sessionKeys
	s.mu.Unlock()
	if list == nil {
		return 0
	}
	keys, err := list()
	if err != nil || len(keys) == 0 {
		return 0
	}

	type keyAge struct {
		key string
		mod int64
	}
	aged := make([]keyAge, 0, len(keys))
	for _, k := range keys {
		// Stat outside the lock: this touches every session file and the store
		// mutex guards every append on the live turn path.
		info, statErr := os.Stat(s.messagesPath(k))
		if statErr != nil {
			continue
		}
		aged = append(aged, keyAge{key: k, mod: info.ModTime().UnixMilli()})
	}
	sort.Slice(aged, func(i, j int) bool {
		if aged[i].mod != aged[j].mod {
			return aged[i].mod > aged[j].mod
		}
		return aged[i].key < aged[j].key // stable for equal mtimes
	})
	if len(aged) > limit {
		aged = aged[:limit]
	}

	added := 0
	for _, a := range aged {
		s.mu.Lock()
		_, resident := s.sessions[a.key]
		if !resident {
			s.ensureSession(a.key)
			added++
		}
		s.mu.Unlock()
	}
	return added
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
