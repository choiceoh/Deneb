package aurora

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ── Summaries ───────────────────────────────────────────────────────────────

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

// ── Transfer tracking (write) ───────────────────────────────────────────────

// MarkTransferred records that a summary has been transferred to the memory store.
func (s *Store) MarkTransferred(summaryID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO transferred_summaries (summary_id, transferred_at) VALUES (?, ?)`,
		summaryID, time.Now().UnixMilli())
	return err
}
