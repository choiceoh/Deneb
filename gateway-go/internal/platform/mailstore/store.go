// Package mailstore is a local, file-backed mirror of the on-box mail archive
// with an in-memory full-text index, so agent reads never pay a per-call IMAP
// round-trip + re-parse + re-index. Messages are shaped once (at LMTP intake or
// during backfill) into mailarchive.ContextMessage — body already cleaned and
// clamped — and persisted as month-sharded JSONL. On startup the shards load
// into a textsearch.Index plus id/locator/message-id maps; thereafter search,
// read, thread, list, and project-history answer from memory.
//
// The store mirrors the wiki/polaris idiom (files + in-memory index, no CGO/
// sqlite). It reuses mailarchive's ranking, clustering, dedupe, and shaping via
// the exported wrappers in mailarchive/export.go so the two paths never drift.
package mailstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
	"github.com/choiceoh/deneb/gateway-go/pkg/textsearch"
)

// Store holds the persisted messages and their in-memory search surface.
type Store struct {
	dir string

	mu       sync.RWMutex
	idx      *textsearch.Index                     // full-text over subject/body/participants/…
	byKey    map[string]mailarchive.ContextMessage // dedupeKey → message (source of truth)
	byLoc    map[string]string                     // Locator → dedupeKey (read by locator)
	byMsgID  map[string]string                     // normalized Message-ID → dedupeKey (thread/read)
	byID     map[string]string                     // ContextMessage.ID → dedupeKey (read by bare id)
	semantic *embedindex.Index                     // optional dense index; never called while mu is held
	reranker MailReranker                          // optional cross-encoder; never called while mu is held
}

// New opens (or creates) the store at dir and loads every JSONL shard into the
// in-memory index. A corrupt shard is skipped, not fatal — a partial archive is
// better than none, and the tool falls back to IMAP for anything missing.
func New(dir string) (*Store, error) {
	msgDir := filepath.Join(dir, "messages")
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		return nil, fmt.Errorf("mailstore: mkdir: %w", err)
	}
	s := &Store{
		dir:     dir,
		idx:     textsearch.New(),
		byKey:   make(map[string]mailarchive.ContextMessage),
		byLoc:   make(map[string]string),
		byMsgID: make(map[string]string),
		byID:    make(map[string]string),
	}
	entries, err := os.ReadDir(msgDir)
	if err != nil {
		return nil, fmt.Errorf("mailstore: read dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		msgs, lerr := jsonlstore.Load[mailarchive.ContextMessage](filepath.Join(msgDir, e.Name()))
		if lerr != nil {
			continue // corrupt shard — skip, IMAP fallback covers the gap
		}
		for i := range msgs {
			mailarchive.RehydrateWhen(&msgs[i]) // JSONL round-trip drops the unexported sort key
			s.putInMemoryLocked(msgs[i])
		}
	}
	return s, nil
}

// Close stops an optional semantic refresh. Message mutations themselves are
// already flushed to their shard at Put time.
func (s *Store) Close() error {
	if semantic := s.semanticSnapshot(); semantic != nil {
		semantic.Close()
	}
	return nil
}

// Len reports how many distinct messages are indexed (0 = not yet backfilled,
// so the tool should IMAP-fallback rather than trust an empty answer).
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byKey)
}

// Put persists a message and indexes it. Idempotent by dedupe key (Message-ID
// preferred): a message already stored is a no-op (returns created=false), so
// backfill re-runs and MTA re-deliveries never double-write. Safe for concurrent
// LMTP intake against tool reads.
func (s *Store) Put(msg mailarchive.ContextMessage) (created bool, err error) {
	mailarchive.RehydrateWhen(&msg) // ensure the sort key is set (defensive: some callers/tests build ContextMessage directly)
	key := mailarchive.ContextMessageDedupeKey(msg)
	if key == "" {
		return false, fmt.Errorf("mailstore: message has no dedupe key")
	}
	s.mu.Lock()
	if existing, ok := s.byKey[key]; ok {
		// Allow a one-way upgrade when a prior metadata-only review stub left an
		// empty body. Full→full and full→empty remain idempotent no-ops.
		if strings.TrimSpace(existing.Body) != "" || strings.TrimSpace(msg.Body) == "" {
			s.mu.Unlock()
			return false, nil
		}
	}
	if aerr := jsonlstore.Append(s.shardPath(msg), msg); aerr != nil {
		s.mu.Unlock()
		return false, fmt.Errorf("mailstore: append: %w", aerr)
	}
	s.putInMemoryLocked(msg)
	semantic := s.semantic
	s.mu.Unlock()
	if semantic != nil && semantic.Enabled() {
		semantic.RefreshAsync(s.semanticItems)
	}
	return true, nil
}

// shardPath returns the month shard a message belongs to (by its sent Date),
// or the "unknown" shard when the date is unparseable.
func (s *Store) shardPath(msg mailarchive.ContextMessage) string {
	name := "unknown"
	if when := mailarchive.ParseMailDate(msg.Date); !when.IsZero() {
		name = when.Format("2006-01")
	}
	return filepath.Join(s.dir, "messages", name+".jsonl")
}

// putInMemoryLocked inserts a message into the index and lookup maps. The caller
// holds the write lock (Put) or is single-threaded (New).
func (s *Store) putInMemoryLocked(msg mailarchive.ContextMessage) {
	key := mailarchive.ContextMessageDedupeKey(msg)
	if key == "" {
		return
	}
	s.byKey[key] = msg
	if msg.Locator != "" {
		s.byLoc[msg.Locator] = key
	}
	if msg.ID != "" {
		s.byID[msg.ID] = key
	}
	if mid := mailarchive.NormalizeMsgID(msg.MessageID); mid != "" {
		s.byMsgID[mid] = key
	}
	s.idx.Upsert(key, mailarchive.ContextIndexFields(msg)...)
}

// matchMailbox reports whether msg is in one of the wanted mailboxes. An empty
// filter matches everything.
func matchMailbox(msg mailarchive.ContextMessage, mailboxes []string) bool {
	if len(mailboxes) == 0 {
		return true
	}
	for _, mb := range mailboxes {
		if strings.EqualFold(strings.TrimSpace(mb), strings.TrimSpace(msg.Mailbox)) {
			return true
		}
	}
	return false
}

// clip caps a slice to limit (limit<=0 means no cap).
func clip(msgs []mailarchive.ContextMessage, limit int) []mailarchive.ContextMessage {
	if limit > 0 && len(msgs) > limit {
		return msgs[:limit]
	}
	return msgs
}
