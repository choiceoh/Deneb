package mailstore

// query.go — the six agent-facing read paths, mirroring the mailarchive tool's
// actions (list/search/read/thread/project_history). All answer from the
// in-memory index/maps under a read lock; the *Locked helpers let one public
// method compose another without re-entering the RWMutex.

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

// List returns messages sent on/after since, newest first, mailbox-filtered.
func (s *Store) List(mailboxes []string, since time.Time, limit int) []mailarchive.ContextMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]mailarchive.ContextMessage, 0, 64)
	for _, msg := range s.byKey {
		if !matchMailbox(msg, mailboxes) {
			continue
		}
		if !since.IsZero() && !mailarchive.SentOnOrAfter(msg.Date, since) {
			continue
		}
		out = append(out, msg)
	}
	mailarchive.SortContextMessages(out, false) // newest first
	return clip(out, limit)
}

// Search returns full-text hits, mailbox/since-filtered, best-match first.
func (s *Store) Search(mailboxes []string, query string, since time.Time, limit int) []mailarchive.ContextMessage {
	// Legacy callers do not carry a request context. Keep their semantic work
	// bounded; runtime tool paths call SearchContext with the turn context.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.SearchContext(ctx, mailboxes, query, since, limit)
}

// SearchContext returns BM25+dense RRF hits, mailbox/since-filtered.
func (s *Store) SearchContext(ctx context.Context, mailboxes []string, query string, since time.Time, limit int) []mailarchive.ContextMessage {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	candidate := limit * 4
	if candidate < 40 {
		candidate = 40
	}
	ranked := s.rankedSearch(ctx, query, candidate)
	s.mu.RLock()
	out := make([]mailarchive.ContextMessage, 0, len(ranked))
	for _, hit := range ranked {
		msg, ok := s.byKey[hit.id]
		if !ok || !matchMailbox(msg, mailboxes) {
			continue
		}
		if !since.IsZero() && !mailarchive.SentOnOrAfter(msg.Date, since) {
			continue
		}
		msg.Score += hit.fusedScore
		msg.RankReasons = append([]string(nil), msg.RankReasons...)
		if hit.lexicalRank > 0 {
			msg.RankReasons = appendMailRankReason(msg.RankReasons, "local_fts")
		}
		if hit.semanticRank > 0 {
			msg.RankReasons = appendMailRankReason(msg.RankReasons, "semantic")
		}
		out = append(out, msg)
	}
	s.mu.RUnlock()
	out = s.rerankMessages(ctx, query, out)
	return clip(out, limit)
}

func (s *Store) searchLocked(mailboxes []string, query string, since time.Time, limit int) []mailarchive.ContextMessage {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	candidate := limit * 4
	if candidate < 40 {
		candidate = 40
	}
	hits := s.idx.Search(query, candidate)
	out := make([]mailarchive.ContextMessage, 0, len(hits))
	for _, h := range hits {
		msg, ok := s.byKey[h.ID]
		if !ok {
			continue
		}
		if !matchMailbox(msg, mailboxes) {
			continue
		}
		if !since.IsZero() && !mailarchive.SentOnOrAfter(msg.Date, since) {
			continue
		}
		msg.Score += h.Score
		out = append(out, msg)
	}
	return clip(out, limit)
}

// Read resolves a locator, Message-ID, bare id, or descriptive query to one
// message. ok=false means the store has no match — the caller IMAP-falls back.
func (s *Store) Read(messageID, query string, mailboxes []string) (mailarchive.ContextMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readLocked(messageID, query, mailboxes)
}

// bodyHitLocked returns a stored message only when it has a readable body.
// Empty-body review stubs are treated as misses so callers fall back to IMAP/
// Gmail instead of serving a permanently blank open.
func (s *Store) bodyHitLocked(key string, mailboxes []string) (mailarchive.ContextMessage, bool) {
	msg, ok := s.byKey[key]
	if !ok || !matchMailbox(msg, mailboxes) || strings.TrimSpace(msg.Body) == "" {
		return mailarchive.ContextMessage{}, false
	}
	return msg, true
}

func (s *Store) readLocked(messageID, query string, mailboxes []string) (mailarchive.ContextMessage, bool) {
	if id := strings.TrimSpace(messageID); id != "" {
		if key, ok := s.byLoc[id]; ok { // it's a locator
			if msg, ok := s.bodyHitLocked(key, mailboxes); ok {
				return msg, true
			}
		}
		if key, ok := s.byMsgID[mailarchive.NormalizeMsgID(id)]; ok { // it's a Message-ID
			if msg, ok := s.bodyHitLocked(key, mailboxes); ok {
				return msg, true
			}
		}
		if key, ok := s.byID[id]; ok { // bare ContextMessage.ID (sanitized Message-ID or fallback)
			if msg, ok := s.bodyHitLocked(key, mailboxes); ok {
				return msg, true
			}
		}
	}
	if q := strings.TrimSpace(query); q != "" {
		if hits := s.searchLocked(mailboxes, q, time.Time{}, 1); len(hits) > 0 {
			return hits[0], true
		}
	}
	return mailarchive.ContextMessage{}, false
}

// Thread walks the References/Message-ID graph from a seed, chronological order.
// ok=false when the seed isn't in the store (caller IMAP-falls back).
func (s *Store) Thread(messageID, query string, mailboxes []string, limit int) ([]mailarchive.ContextMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seed, ok := s.readLocked(messageID, query, mailboxes)
	if !ok {
		return nil, false
	}
	if limit <= 0 {
		limit = 40
	}
	// A message belongs to the thread if its Message-ID is in the thread's id
	// set OR any of its References points into it. Both edges matter: backward
	// (a reply cites the seed via References) and forward (the seed's References
	// name its parents). Iterate to a fixpoint over the corpus — thread reads are
	// infrequent, so a bounded scan beats maintaining a references reverse-index.
	threadIDs := map[string]bool{}
	markIDs := func(msg mailarchive.ContextMessage) {
		if mid := mailarchive.NormalizeMsgID(msg.MessageID); mid != "" {
			threadIDs[mid] = true
		}
		for _, ref := range msg.References {
			if r := mailarchive.NormalizeMsgID(ref); r != "" {
				threadIDs[r] = true
			}
		}
	}
	markIDs(seed)
	var out []mailarchive.ContextMessage
	seen := map[string]bool{mailarchive.ContextMessageDedupeKey(seed): true}
	out = append(out, seed)
	for changed := true; changed; {
		changed = false
		for _, msg := range s.byKey {
			if !matchMailbox(msg, mailboxes) {
				continue
			}
			key := mailarchive.ContextMessageDedupeKey(msg)
			if seen[key] {
				continue
			}
			inThread := threadIDs[mailarchive.NormalizeMsgID(msg.MessageID)]
			for _, ref := range msg.References {
				if inThread {
					break
				}
				inThread = threadIDs[mailarchive.NormalizeMsgID(ref)]
			}
			if !inThread {
				continue
			}
			seen[key] = true
			out = append(out, msg)
			markIDs(msg) // this message's id can now pull in its own replies
			changed = true
		}
	}
	mailarchive.SortContextMessages(out, true) // chronological
	if len(out) > limit {
		out = out[len(out)-limit:] // keep the most recent window
	}
	return out, true
}

// ProjectHistory ranks project/company/person hits into a timeline + thread
// clusters. ok=false when the index is empty or nothing matched (IMAP fallback).
func (s *Store) ProjectHistory(query string, since time.Time, limit, indexLimit int) (mailarchive.ProjectHistory, bool) {
	// See Search: preserve the context-free API for backfill/tests while keeping
	// the detached semantic request bounded.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.ProjectHistoryContext(ctx, query, since, limit, indexLimit)
}

// ProjectHistoryContext builds project history from the same hybrid candidate
// ranking as SearchContext, then applies the existing deterministic business
// signal ranker and thread clustering.
func (s *Store) ProjectHistoryContext(ctx context.Context, query string, since time.Time, limit, indexLimit int) (mailarchive.ProjectHistory, bool) {
	s.mu.RLock()
	empty := s.idx.Len() == 0
	s.mu.RUnlock()
	if empty {
		return mailarchive.ProjectHistory{}, false // not backfilled yet
	}
	if limit <= 0 {
		limit = 40
	}
	candidate := indexLimit
	if candidate <= 0 {
		if candidate = limit * 8; candidate < 200 {
			candidate = 200
		}
	}
	rankedHits := s.rankedSearch(ctx, query, candidate)
	s.mu.RLock()
	msgs := make([]mailarchive.ContextMessage, 0, len(rankedHits))
	for _, hit := range rankedHits {
		msg, ok := s.byKey[hit.id]
		if !ok {
			continue
		}
		if !since.IsZero() && !mailarchive.SentOnOrAfter(msg.Date, since) {
			continue
		}
		msg.Score += hit.fusedScore
		msg.RankReasons = append([]string(nil), msg.RankReasons...)
		if hit.lexicalRank > 0 {
			msg.RankReasons = appendMailRankReason(msg.RankReasons, "local_fts")
		}
		if hit.semanticRank > 0 {
			msg.RankReasons = appendMailRankReason(msg.RankReasons, "semantic")
		}
		msgs = append(msgs, msg)
	}
	s.mu.RUnlock()
	if len(msgs) == 0 {
		return mailarchive.ProjectHistory{}, false
	}
	ranked := mailarchive.RankProjectMessages(query, msgs)
	ranked = s.rerankMessages(ctx, query, ranked)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	mailarchive.SortContextMessages(ranked, false)
	return mailarchive.ProjectHistory{
		Query:     query,
		Messages:  ranked,
		Threads:   mailarchive.ClusterProjectThreads(ranked),
		IndexUsed: true,
	}, true
}
