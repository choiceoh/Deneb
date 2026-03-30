package aurora

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

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

// ── Transfer tracking (read) ───────────────────────────────────────────────

// IsTransferred checks if a summary has already been transferred to the memory store.
func (s *Store) IsTransferred(summaryID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM transferred_summaries WHERE summary_id = ?`, summaryID).Scan(&count)
	return count > 0
}
