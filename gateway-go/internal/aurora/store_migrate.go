package aurora

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// storeData is the legacy JSON on-disk schema, used only for migration.
type storeData struct {
	ContextItems         []ContextItem            `json:"contextItems"`
	Messages             map[string]MessageRecord `json:"messages"`
	Summaries            map[string]SummaryRecord `json:"summaries"`
	SummaryParents       map[string][]string      `json:"summaryParents"`
	SummaryMessages      map[string][]uint64      `json:"summaryMessages"`
	CompactionEvents     []CompactionEvent        `json:"compactionEvents"`
	TransferredSummaries map[string]int64         `json:"transferredSummaries"`
	NextOrdinalVal       uint64                   `json:"nextOrdinal"`
	NextMessageID        uint64                   `json:"nextMessageId"`
}

// migrateFromJSON imports data from a legacy aurora.json file if it exists.
// After successful import, the JSON file is renamed to aurora.json.migrated.
func (s *Store) migrateFromJSON(dir string) error {
	// Check common legacy paths: same dir as DB, or the old default.
	candidates := []string{
		filepath.Join(dir, "aurora.json"),
	}
	// If the DB path changed from the old default, also check the old path.
	home, _ := os.UserHomeDir()
	oldDefault := filepath.Join(home, ".deneb", "aurora.json")
	if dir != filepath.Join(home, ".deneb") {
		candidates = append(candidates, oldDefault)
	}

	var jsonPath string
	var raw []byte
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil && len(data) > 0 {
			jsonPath = p
			raw = data
			break
		}
	}
	if jsonPath == "" {
		return nil // no legacy file
	}

	// Check if we already have data (migration already happened).
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM context_items`).Scan(&count)
	if count > 0 {
		return nil // already migrated
	}

	var legacy storeData
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return fmt.Errorf("parse legacy json: %w", err)
	}

	s.logger.Info("aurora store: migrating from JSON", "path", jsonPath,
		"items", len(legacy.ContextItems),
		"messages", len(legacy.Messages),
		"summaries", len(legacy.Summaries))

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer tx.Rollback()

	// Import sequences.
	tx.Exec(`INSERT OR REPLACE INTO sequences (name, value) VALUES ('ordinal', ?)`, legacy.NextOrdinalVal)
	tx.Exec(`INSERT OR REPLACE INTO sequences (name, value) VALUES ('message_id', ?)`, legacy.NextMessageID)

	// Import context items.
	ciStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO context_items (conversation_id, ordinal, item_type, message_id, summary_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`)
	defer ciStmt.Close()
	for _, ci := range legacy.ContextItems {
		var msgID, sumID any
		if ci.MessageID != nil {
			msgID = *ci.MessageID
		}
		if ci.SummaryID != nil {
			sumID = *ci.SummaryID
		}
		ciStmt.Exec(ci.ConversationID, ci.Ordinal, ci.ItemType, msgID, sumID, ci.CreatedAt)
	}

	// Import messages.
	msgStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO messages (message_id, conversation_id, seq, role, content, token_count, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	defer msgStmt.Close()
	for _, m := range legacy.Messages {
		msgStmt.Exec(m.MessageID, m.ConversationID, m.Seq, m.Role, m.Content, m.TokenCount, m.CreatedAt)
	}

	// Import summaries.
	sumStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO summaries (summary_id, conversation_id, kind, depth, content, token_count, file_ids, earliest_at, latest_at, descendant_count, descendant_token_count, source_message_token_count, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	defer sumStmt.Close()
	for _, sr := range legacy.Summaries {
		fileIDsJSON, _ := json.Marshal(sr.FileIDs)
		sumStmt.Exec(sr.SummaryID, sr.ConversationID, sr.Kind, sr.Depth, sr.Content, sr.TokenCount, string(fileIDsJSON), sr.EarliestAt, sr.LatestAt, sr.DescendantCount, sr.DescendantTokenCount, sr.SourceMessageTokenCount, sr.CreatedAt)
	}

	// Import summary parents.
	spStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO summary_parents (summary_id, parent_id) VALUES (?, ?)`)
	defer spStmt.Close()
	for sid, parents := range legacy.SummaryParents {
		for _, pid := range parents {
			spStmt.Exec(sid, pid)
		}
	}

	// Import summary messages.
	smStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO summary_messages (summary_id, message_id) VALUES (?, ?)`)
	defer smStmt.Close()
	for sid, msgIDs := range legacy.SummaryMessages {
		for _, mid := range msgIDs {
			smStmt.Exec(sid, mid)
		}
	}

	// Import compaction events.
	ceStmt, _ := tx.Prepare(`INSERT INTO compaction_events (conversation_id, pass, level, tokens_before, tokens_after, created_summary_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	defer ceStmt.Close()
	for _, e := range legacy.CompactionEvents {
		ceStmt.Exec(e.ConversationID, e.Pass, e.Level, e.TokensBefore, e.TokensAfter, e.CreatedSummaryID, e.CreatedAt)
	}

	// Import transferred summaries.
	tsStmt, _ := tx.Prepare(`INSERT OR IGNORE INTO transferred_summaries (summary_id, transferred_at) VALUES (?, ?)`)
	defer tsStmt.Close()
	for sid, ts := range legacy.TransferredSummaries {
		tsStmt.Exec(sid, ts)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}

	// Rename legacy file so we don't re-migrate.
	if err := os.Rename(jsonPath, jsonPath+".migrated"); err != nil {
		s.logger.Warn("aurora store: could not rename legacy json", "error", err)
	} else {
		s.logger.Info("aurora store: migration complete, renamed to .migrated", "path", jsonPath)
	}

	return nil
}
