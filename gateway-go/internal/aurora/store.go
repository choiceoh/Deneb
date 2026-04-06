// Aurora context store backed by SQLite (WAL mode).
//
// Manages context_items, messages, summaries, and compaction_events
// that power the Rust Aurora hierarchical compaction engine via FFI.
// Migrated from single-file JSON persistence to SQLite for indexed
// lookups and transactional consistency.
// Optimized for single-user deployment (no concurrent access concerns).
package aurora

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// maxCompactionEvents is the maximum number of compaction events retained.
// Older entries are pruned to prevent unbounded growth.
const maxCompactionEvents = 500

// Store is the Aurora context store (SQLite-backed).
type Store struct {
	mu     sync.RWMutex
	db     *sql.DB
	dbPath string
	logger *slog.Logger

	closeOnce sync.Once
	closeErr  error
}

// CompactionEvent is a persisted compaction event record.
type CompactionEvent struct {
	ConversationID   uint64 `json:"conversationId"`
	Pass             string `json:"pass"`
	Level            string `json:"level"`
	TokensBefore     uint64 `json:"tokensBefore"`
	TokensAfter      uint64 `json:"tokensAfter"`
	CreatedSummaryID string `json:"createdSummaryId"`
	CreatedAt        int64  `json:"createdAt"`
}

// ── Data types matching Rust core-rs types ──────────────────────────────────

// ContextItem corresponds to core-rs compaction::ContextItem.
type ContextItem struct {
	ConversationID uint64  `json:"conversationId"`
	Ordinal        uint64  `json:"ordinal"`
	ItemType       string  `json:"itemType"` // "message" or "summary"
	MessageID      *uint64 `json:"messageId,omitempty"`
	SummaryID      *string `json:"summaryId,omitempty"`
	CreatedAt      int64   `json:"createdAt"` // epoch ms
}

// MessageRecord corresponds to core-rs compaction::MessageRecord.
type MessageRecord struct {
	MessageID      uint64 `json:"messageId"`
	ConversationID uint64 `json:"conversationId"`
	Seq            uint64 `json:"seq"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	TokenCount     uint64 `json:"tokenCount"`
	CreatedAt      int64  `json:"createdAt"` // epoch ms
}

// SummaryRecord corresponds to core-rs compaction::SummaryRecord.
type SummaryRecord struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Kind                    string   `json:"kind"` // "leaf" or "condensed"
	Depth                   uint32   `json:"depth"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	DescendantCount         uint64   `json:"descendantCount"`
	DescendantTokenCount    uint64   `json:"descendantTokenCount"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	CreatedAt               int64    `json:"createdAt"` // epoch ms
}

// SummaryStats holds aggregate summary info for context assembly.
type SummaryStats struct {
	MaxDepth           uint32 `json:"maxDepth"`
	CondensedCount     int    `json:"condensedCount"`
	LeafCount          int    `json:"leafCount"`
	TotalSummaryTokens uint64 `json:"totalSummaryTokens"`
}

// PersistLeafInput matches core-rs sweep::PersistLeafInput.
type PersistLeafInput struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	MessageIDs              []uint64 `json:"messageIds"`
	StartOrdinal            uint64   `json:"startOrdinal"`
	EndOrdinal              uint64   `json:"endOrdinal"`
}

// PersistCondensedInput matches core-rs sweep::PersistCondensedInput.
type PersistCondensedInput struct {
	SummaryID               string   `json:"summaryId"`
	ConversationID          uint64   `json:"conversationId"`
	Depth                   uint32   `json:"depth"`
	Content                 string   `json:"content"`
	TokenCount              uint64   `json:"tokenCount"`
	FileIDs                 []string `json:"fileIds"`
	EarliestAt              *int64   `json:"earliestAt,omitempty"`
	LatestAt                *int64   `json:"latestAt,omitempty"`
	DescendantCount         uint64   `json:"descendantCount"`
	DescendantTokenCount    uint64   `json:"descendantTokenCount"`
	SourceMessageTokenCount uint64   `json:"sourceMessageTokenCount"`
	ParentSummaryIDs        []string `json:"parentSummaryIds"`
	StartOrdinal            uint64   `json:"startOrdinal"`
	EndOrdinal              uint64   `json:"endOrdinal"`
}

// PersistEventInput matches core-rs sweep::PersistEventInput.
type PersistEventInput struct {
	ConversationID   uint64 `json:"conversationId"`
	Pass             string `json:"pass"`
	Level            string `json:"level"`
	TokensBefore     uint64 `json:"tokensBefore"`
	TokensAfter      uint64 `json:"tokensAfter"`
	CreatedSummaryID string `json:"createdSummaryId"`
}

// ── Constructor ─────────────────────────────────────────────────────────────

// NewStoreFromDB creates an Aurora store using a pre-opened database connection.
// Used by the unified store to share a single DB across subsystems.
// The caller owns the DB lifecycle — Close() on this store is a no-op.
func NewStoreFromDB(db *sql.DB, logger *slog.Logger) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Store{
		db:     db,
		dbPath: "(shared)",
		logger: logger,
	}
	return s, nil
}

// ── Transfer tracking ──────────────────────────────────────────────────────

// MarkTransferred records that a summary has been transferred to the memory store.
func (s *Store) MarkTransferred(summaryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO transferred_summaries (summary_id, transferred_at) VALUES (?, ?)`,
		summaryID, time.Now().UnixMilli())
	return err
}

// IsTransferred checks if a summary has already been transferred to the memory store.
func (s *Store) IsTransferred(summaryID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM transferred_summaries WHERE summary_id = ?`, summaryID).Scan(&count)
	return count > 0
}

// Sync is a no-op for SQLite (WAL mode auto-checkpoints).
// Retained for API compatibility.
func (s *Store) Sync() error {
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		// Don't close shared DB connections (managed by unified store).
		if s.db != nil && s.dbPath != "(shared)" {
			s.closeErr = s.db.Close()
		}
	})
	return s.closeErr
}

// ── Sequences ──────────────────────────────────────────────────────────────

// nextSequence atomically increments and returns the next value for a named sequence.
// Caller must hold s.mu write lock.
func (s *Store) nextSequence(tx *sql.Tx, name string) (uint64, error) {
	// Ensure the row exists.
	tx.Exec(`INSERT OR IGNORE INTO sequences (name, value) VALUES (?, 0)`, name)
	var val uint64
	if err := tx.QueryRow(`SELECT value FROM sequences WHERE name = ?`, name).Scan(&val); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE sequences SET value = ? WHERE name = ?`, val+1, name); err != nil {
		return 0, err
	}
	return val, nil
}

// ── Context items ───────────────────────────────────────────────────────────

// FetchContextItems returns all context items for a conversation ordered by ordinal.
func (s *Store) FetchContextItems(conversationID uint64) ([]ContextItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT conversation_id, ordinal, item_type, message_id, summary_id, created_at
		 FROM context_items WHERE conversation_id = ? ORDER BY ordinal`,
		conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ContextItem
	for rows.Next() {
		var ci ContextItem
		var msgID sql.NullInt64
		var sumID sql.NullString
		if err := rows.Scan(&ci.ConversationID, &ci.Ordinal, &ci.ItemType, &msgID, &sumID, &ci.CreatedAt); err != nil {
			return nil, err
		}
		if msgID.Valid {
			v := uint64(msgID.Int64)
			ci.MessageID = &v
		}
		if sumID.Valid {
			ci.SummaryID = &sumID.String
		}
		items = append(items, ci)
	}
	return items, rows.Err()
}

// NextOrdinal returns the next available ordinal.
func (s *Store) NextOrdinal(conversationID uint64) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var val uint64
	err := s.db.QueryRow(`SELECT COALESCE((SELECT value FROM sequences WHERE name = 'ordinal'), 0)`).Scan(&val)
	return val, err
}

// ── Messages ────────────────────────────────────────────────────────────────

// FetchMessages returns messages by their IDs as a map.
func (s *Store) FetchMessages(messageIDs []uint64) (map[uint64]MessageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[uint64]MessageRecord, len(messageIDs))
	if len(messageIDs) == 0 {
		return result, nil
	}

	// Build query with placeholders.
	placeholders := make([]string, len(messageIDs))
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT message_id, conversation_id, seq, role, content, token_count, created_at
		 FROM messages WHERE message_id IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var m MessageRecord
		if err := rows.Scan(&m.MessageID, &m.ConversationID, &m.Seq, &m.Role, &m.Content, &m.TokenCount, &m.CreatedAt); err != nil {
			return nil, err
		}
		result[m.MessageID] = m
	}
	return result, rows.Err()
}

// FetchTokenCount returns total token count for all context items in a conversation.
func (s *Store) FetchTokenCount(conversationID uint64) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total uint64

	// Sum message tokens via join.
	var msgTokens sql.NullInt64
	s.db.QueryRow(
		`SELECT COALESCE(SUM(m.token_count), 0) FROM context_items ci
		 JOIN messages m ON ci.message_id = m.message_id
		 WHERE ci.conversation_id = ? AND ci.item_type = 'message'`,
		conversationID).Scan(&msgTokens)
	if msgTokens.Valid {
		total += uint64(msgTokens.Int64)
	}

	// Sum summary tokens via join.
	var sumTokens sql.NullInt64
	s.db.QueryRow(
		`SELECT COALESCE(SUM(s.token_count), 0) FROM context_items ci
		 JOIN summaries s ON ci.summary_id = s.summary_id
		 WHERE ci.conversation_id = ? AND ci.item_type = 'summary'`,
		conversationID).Scan(&sumTokens)
	if sumTokens.Valid {
		total += uint64(sumTokens.Int64)
	}

	return total, nil
}

// ── Summaries ───────────────────────────────────────────────────────────────

// FetchSummaries returns summaries by their IDs as a map.
func (s *Store) FetchSummaries(summaryIDs []string) (map[string]SummaryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]SummaryRecord, len(summaryIDs))
	if len(summaryIDs) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(summaryIDs))
	args := make([]any, len(summaryIDs))
	for i, id := range summaryIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf(
		`SELECT summary_id, conversation_id, kind, depth, content, token_count,
		        file_ids, earliest_at, latest_at, descendant_count,
		        descendant_token_count, source_message_token_count, created_at
		 FROM summaries WHERE summary_id IN (%s)`,
		strings.Join(placeholders, ","))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sr SummaryRecord
		var fileIDsJSON string
		var earliestAt, latestAt sql.NullInt64
		if err := rows.Scan(&sr.SummaryID, &sr.ConversationID, &sr.Kind, &sr.Depth,
			&sr.Content, &sr.TokenCount, &fileIDsJSON, &earliestAt, &latestAt,
			&sr.DescendantCount, &sr.DescendantTokenCount, &sr.SourceMessageTokenCount,
			&sr.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(fileIDsJSON), &sr.FileIDs)
		if sr.FileIDs == nil {
			sr.FileIDs = []string{}
		}
		if earliestAt.Valid {
			sr.EarliestAt = &earliestAt.Int64
		}
		if latestAt.Valid {
			sr.LatestAt = &latestAt.Int64
		}
		result[sr.SummaryID] = sr
	}
	return result, rows.Err()
}

// FetchRecentSummaries returns the N most recent condensed summaries
// (depth >= 1) ordered by created_at descending.
func (s *Store) FetchRecentSummaries(limit int) ([]SummaryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.Query(
		`SELECT summary_id, conversation_id, kind, depth, content, token_count,
		        file_ids, earliest_at, latest_at, descendant_count,
		        descendant_token_count, source_message_token_count, created_at
		 FROM summaries WHERE depth >= 1
		 ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SummaryRecord
	for rows.Next() {
		var sr SummaryRecord
		var fileIDsJSON string
		var earliestAt, latestAt sql.NullInt64
		if err := rows.Scan(&sr.SummaryID, &sr.ConversationID, &sr.Kind, &sr.Depth,
			&sr.Content, &sr.TokenCount, &fileIDsJSON, &earliestAt, &latestAt,
			&sr.DescendantCount, &sr.DescendantTokenCount, &sr.SourceMessageTokenCount,
			&sr.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(fileIDsJSON), &sr.FileIDs)
		if sr.FileIDs == nil {
			sr.FileIDs = []string{}
		}
		if earliestAt.Valid {
			sr.EarliestAt = &earliestAt.Int64
		}
		if latestAt.Valid {
			sr.LatestAt = &latestAt.Int64
		}
		result = append(result, sr)
	}
	return result, rows.Err()
}

// FetchDistinctDepths returns distinct summary depths for context items
// with ordinal <= maxOrdinal.
func (s *Store) FetchDistinctDepths(conversationID uint64, maxOrdinal uint64) ([]uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT DISTINCT sm.depth FROM context_items ci
		 JOIN summaries sm ON ci.summary_id = sm.summary_id
		 WHERE ci.conversation_id = ? AND ci.ordinal <= ? AND ci.item_type = 'summary'
		 ORDER BY sm.depth`,
		conversationID, maxOrdinal)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var depths []uint32
	for rows.Next() {
		var d uint32
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		depths = append(depths, d)
	}
	return depths, rows.Err()
}

// FetchSummaryStats returns aggregate summary info for a conversation.
func (s *Store) FetchSummaryStats(conversationID uint64) (SummaryStats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var stats SummaryStats
	s.db.QueryRow(
		`SELECT COALESCE(MAX(depth), 0),
		        COALESCE(SUM(CASE WHEN kind = 'condensed' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN kind != 'condensed' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(token_count), 0)
		 FROM summaries WHERE conversation_id = ?`,
		conversationID).Scan(&stats.MaxDepth, &stats.CondensedCount, &stats.LeafCount, &stats.TotalSummaryTokens)
	return stats, nil
}

// PersistLeafSummary inserts a leaf summary and replaces compacted messages.
func (s *Store) PersistLeafSummary(input PersistLeafInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UnixMilli()
	fileIDs := input.FileIDs
	if fileIDs == nil {
		fileIDs = []string{}
	}
	fileIDsJSON, _ := json.Marshal(fileIDs)

	// Insert summary record.
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO summaries
		 (summary_id, conversation_id, kind, depth, content, token_count, file_ids,
		  earliest_at, latest_at, descendant_count, descendant_token_count,
		  source_message_token_count, created_at)
		 VALUES (?, ?, 'leaf', 0, ?, ?, ?, ?, ?, 0, 0, ?, ?)`,
		input.SummaryID, input.ConversationID, input.Content, input.TokenCount,
		string(fileIDsJSON), input.EarliestAt, input.LatestAt,
		input.SourceMessageTokenCount, now); err != nil {
		return err
	}

	// Parse structured XML sections and store in extended columns.
	// Best-effort: columns may not exist in legacy aurora.db.
	parsed := ParseStructuredSummary(input.Content)
	tx.Exec(
		`UPDATE summaries SET narrative = ?, decisions = ?, pending = ?, refs = ?,
		 goal = ?, next_steps = ?, critical_context = ?
		 WHERE summary_id = ?`,
		parsed.Narrative, parsed.Decisions, parsed.Pending, parsed.Refs,
		parsed.Goal, parsed.NextSteps, parsed.CriticalContext, input.SummaryID)

	// Link messages to summary.
	smStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO summary_messages (summary_id, message_id) VALUES (?, ?)`)
	defer smStmt.Close()
	for _, mid := range input.MessageIDs {
		smStmt.Exec(input.SummaryID, mid)
	}

	// Remove compacted message context items in ordinal range.
	if _, err := tx.Exec(
		`DELETE FROM context_items
		 WHERE conversation_id = ? AND ordinal >= ? AND ordinal <= ? AND item_type = 'message'`,
		input.ConversationID, input.StartOrdinal, input.EndOrdinal); err != nil {
		return err
	}

	// Insert summary context item at start ordinal.
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO context_items (conversation_id, ordinal, item_type, summary_id, created_at)
		 VALUES (?, ?, 'summary', ?, ?)`,
		input.ConversationID, input.StartOrdinal, input.SummaryID, now); err != nil {
		return err
	}

	// Prune compacted message records.
	if len(input.MessageIDs) > 0 {
		ph := make([]string, len(input.MessageIDs))
		args := make([]any, len(input.MessageIDs))
		for i, id := range input.MessageIDs {
			ph[i] = "?"
			args[i] = id
		}
		tx.Exec(fmt.Sprintf(`DELETE FROM messages WHERE message_id IN (%s)`, strings.Join(ph, ",")), args...)
	}

	return tx.Commit()
}

// PersistCondensedSummary inserts a condensed summary and replaces children.
func (s *Store) PersistCondensedSummary(input PersistCondensedInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UnixMilli()
	fileIDs := input.FileIDs
	if fileIDs == nil {
		fileIDs = []string{}
	}
	fileIDsJSON, _ := json.Marshal(fileIDs)

	// Insert condensed summary record.
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO summaries
		 (summary_id, conversation_id, kind, depth, content, token_count, file_ids,
		  earliest_at, latest_at, descendant_count, descendant_token_count,
		  source_message_token_count, created_at)
		 VALUES (?, ?, 'condensed', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		input.SummaryID, input.ConversationID, input.Depth, input.Content,
		input.TokenCount, string(fileIDsJSON), input.EarliestAt, input.LatestAt,
		input.DescendantCount, input.DescendantTokenCount,
		input.SourceMessageTokenCount, now); err != nil {
		return err
	}

	// Parse structured XML sections and store in extended columns.
	parsed := ParseStructuredSummary(input.Content)
	tx.Exec(
		`UPDATE summaries SET narrative = ?, decisions = ?, pending = ?, refs = ?
		 WHERE summary_id = ?`,
		parsed.Narrative, parsed.Decisions, parsed.Pending, parsed.Refs, input.SummaryID)

	// Link parent summaries.
	spStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO summary_parents (summary_id, parent_id) VALUES (?, ?)`)
	defer spStmt.Close()
	for _, pid := range input.ParentSummaryIDs {
		spStmt.Exec(input.SummaryID, pid)
	}

	// Remove condensed child context items in ordinal range.
	if _, err := tx.Exec(
		`DELETE FROM context_items
		 WHERE conversation_id = ? AND ordinal >= ? AND ordinal <= ? AND item_type = 'summary'`,
		input.ConversationID, input.StartOrdinal, input.EndOrdinal); err != nil {
		return err
	}

	// Insert condensed summary context item.
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO context_items (conversation_id, ordinal, item_type, summary_id, created_at)
		 VALUES (?, ?, 'summary', ?, ?)`,
		input.ConversationID, input.StartOrdinal, input.SummaryID, now); err != nil {
		return err
	}

	// Prune parent summary records and their links.
	for _, parentID := range input.ParentSummaryIDs {
		// Delete messages linked to parent summaries.
		rows, _ := tx.Query(`SELECT message_id FROM summary_messages WHERE summary_id = ?`, parentID)
		if rows != nil {
			var msgIDs []any
			for rows.Next() {
				var mid uint64
				rows.Scan(&mid)
				msgIDs = append(msgIDs, mid)
			}
			rows.Close()
			if len(msgIDs) > 0 {
				ph := make([]string, len(msgIDs))
				for i := range ph {
					ph[i] = "?"
				}
				tx.Exec(fmt.Sprintf(`DELETE FROM messages WHERE message_id IN (%s)`, strings.Join(ph, ",")), msgIDs...)
			}
		}
		tx.Exec(`DELETE FROM summaries WHERE summary_id = ?`, parentID)
		tx.Exec(`DELETE FROM summary_parents WHERE summary_id = ?`, parentID)
		tx.Exec(`DELETE FROM summary_messages WHERE summary_id = ?`, parentID)
		tx.Exec(`DELETE FROM transferred_summaries WHERE summary_id = ?`, parentID)
	}

	return tx.Commit()
}

// PersistEvent records a compaction event.
func (s *Store) PersistEvent(input PersistEventInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	if _, err := s.db.Exec(
		`INSERT INTO compaction_events (conversation_id, pass, level, tokens_before, tokens_after, created_summary_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		input.ConversationID, input.Pass, input.Level,
		input.TokensBefore, input.TokensAfter, input.CreatedSummaryID, now); err != nil {
		return err
	}

	// Cap the event log.
	s.db.Exec(
		`DELETE FROM compaction_events WHERE id NOT IN (
			SELECT id FROM compaction_events ORDER BY id DESC LIMIT ?
		)`, maxCompactionEvents)

	return nil
}

// ── Sync from chat ──────────────────────────────────────────────────────────

// SyncMessage ensures a chat message is tracked in the Aurora store.
func (s *Store) SyncMessage(conversationID uint64, role, content string, tokenCount uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := time.Now().UnixMilli()

	ordinal, err := s.nextSequence(tx, "ordinal")
	if err != nil {
		return 0, fmt.Errorf("next ordinal: %w", err)
	}

	msgID, err := s.nextSequence(tx, "message_id")
	if err != nil {
		return 0, fmt.Errorf("next message_id: %w", err)
	}

	if _, err := tx.Exec(
		`INSERT INTO messages (message_id, conversation_id, seq, role, content, token_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		msgID, conversationID, ordinal, role, content, tokenCount, now); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(
		`INSERT INTO context_items (conversation_id, ordinal, item_type, message_id, created_at)
		 VALUES (?, ?, 'message', ?, ?)`,
		conversationID, ordinal, msgID, now); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return msgID, nil
}

// Reset clears all data for a conversation.
func (s *Store) Reset(conversationID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Collect summary IDs for this conversation to clean up links.
	rows, err := tx.Query(`SELECT summary_id FROM summaries WHERE conversation_id = ?`, conversationID)
	if err != nil {
		return err
	}
	var sumIDs []string
	for rows.Next() {
		var sid string
		rows.Scan(&sid)
		sumIDs = append(sumIDs, sid)
	}
	rows.Close()

	// Delete links for each summary.
	for _, sid := range sumIDs {
		tx.Exec(`DELETE FROM summary_parents WHERE summary_id = ?`, sid)
		tx.Exec(`DELETE FROM summary_messages WHERE summary_id = ?`, sid)
		tx.Exec(`DELETE FROM transferred_summaries WHERE summary_id = ?`, sid)
	}

	tx.Exec(`DELETE FROM context_items WHERE conversation_id = ?`, conversationID)
	tx.Exec(`DELETE FROM messages WHERE conversation_id = ?`, conversationID)
	tx.Exec(`DELETE FROM summaries WHERE conversation_id = ?`, conversationID)
	tx.Exec(`DELETE FROM compaction_events WHERE conversation_id = ?`, conversationID)

	return tx.Commit()
}
