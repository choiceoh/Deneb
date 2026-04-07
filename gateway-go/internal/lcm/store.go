package lcm

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/tokenutil"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"

	_ "modernc.org/sqlite"
)

// schema defines all LCM tables and FTS indexes.
// Messages are append-only (immutable). Summary nodes form a DAG via parent_id.
const schema = `
CREATE TABLE IF NOT EXISTS messages (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	session_key TEXT    NOT NULL,
	role        TEXT    NOT NULL,
	content     TEXT    NOT NULL,
	timestamp   INTEGER NOT NULL,
	token_est   INTEGER NOT NULL,
	msg_index   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_msg_session ON messages(session_key, msg_index);

CREATE TABLE IF NOT EXISTS summary_nodes (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	session_key TEXT    NOT NULL,
	level       INTEGER NOT NULL DEFAULT 1,
	content     TEXT    NOT NULL,
	token_est   INTEGER NOT NULL,
	created_at  INTEGER NOT NULL,
	msg_start   INTEGER NOT NULL,
	msg_end     INTEGER NOT NULL,
	parent_id   INTEGER REFERENCES summary_nodes(id)
);
CREATE INDEX IF NOT EXISTS idx_summary_session ON summary_nodes(session_key, level, msg_start);

CREATE VIRTUAL TABLE IF NOT EXISTS lcm_fts USING fts5(
	session_key, role, content,
	content='messages', content_rowid='id',
	tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER IF NOT EXISTS lcm_msg_ai AFTER INSERT ON messages BEGIN
	INSERT INTO lcm_fts(rowid, session_key, role, content)
	VALUES (new.id, new.session_key, new.role, new.content);
END;
CREATE TRIGGER IF NOT EXISTS lcm_msg_ad AFTER DELETE ON messages BEGIN
	INSERT INTO lcm_fts(lcm_fts, rowid, session_key, role, content)
	VALUES ('delete', old.id, old.session_key, old.role, old.content);
END;
`

// Store is the SQLite-backed immutable message store and summary DAG.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore opens (or creates) the LCM database at dbPath.
// Uses WAL mode and busy_timeout for safe concurrent reads.
func NewStore(dbPath string) (*Store, error) {
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("lcm: open db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("lcm: init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// AppendMessage inserts a ChatMessage into the immutable store.
// msg_index is auto-assigned as max(msg_index)+1 for the session.
func (s *Store) AppendMessage(sessionKey string, msg toolctx.ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	content := string(msg.Content)
	tokenEst := tokenutil.EstimateTokens(content)
	ts := msg.Timestamp
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}

	_, err := s.db.Exec(`
		INSERT INTO messages (session_key, role, content, timestamp, token_est, msg_index)
		VALUES (?, ?, ?, ?, ?, COALESCE(
			(SELECT MAX(msg_index) + 1 FROM messages WHERE session_key = ?), 0
		))`,
		sessionKey, msg.Role, content, ts, tokenEst, sessionKey,
	)
	if err != nil {
		return fmt.Errorf("lcm: append message: %w", err)
	}
	return nil
}

// MessageCount returns the number of messages for a session.
func (s *Store) MessageCount(sessionKey string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE session_key = ?`, sessionKey,
	).Scan(&count)
	return count, err
}

// SessionTokens returns the total estimated tokens for a session's messages.
func (s *Store) SessionTokens(sessionKey string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total sql.NullInt64
	err := s.db.QueryRow(
		`SELECT SUM(token_est) FROM messages WHERE session_key = ?`, sessionKey,
	).Scan(&total)
	return int(total.Int64), err
}

// LoadMessages returns messages in [startIdx, endIdx] range (inclusive).
// If endIdx < 0, loads from startIdx to the end.
func (s *Store) LoadMessages(sessionKey string, startIdx, endIdx int) ([]toolctx.ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rows *sql.Rows
	var err error
	if endIdx < 0 {
		rows, err = s.db.Query(
			`SELECT role, content, timestamp FROM messages
			 WHERE session_key = ? AND msg_index >= ?
			 ORDER BY msg_index ASC`, sessionKey, startIdx,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT role, content, timestamp FROM messages
			 WHERE session_key = ? AND msg_index >= ? AND msg_index <= ?
			 ORDER BY msg_index ASC`, sessionKey, startIdx, endIdx,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("lcm: load messages: %w", err)
	}
	defer rows.Close()

	var msgs []toolctx.ChatMessage
	for rows.Next() {
		var role, content string
		var ts int64
		if err := rows.Scan(&role, &content, &ts); err != nil {
			return nil, fmt.Errorf("lcm: scan message: %w", err)
		}
		msgs = append(msgs, toolctx.ChatMessage{
			Role:      role,
			Content:   json.RawMessage(content),
			Timestamp: ts,
		})
	}
	return msgs, rows.Err()
}

// InsertSummary stores a summary node and returns its auto-generated ID.
func (s *Store) InsertSummary(node SummaryNode) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`
		INSERT INTO summary_nodes (session_key, level, content, token_est, created_at, msg_start, msg_end, parent_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		node.SessionKey, node.Level, node.Content, node.TokenEst,
		node.CreatedAt, node.MsgStart, node.MsgEnd, node.ParentID,
	)
	if err != nil {
		return 0, fmt.Errorf("lcm: insert summary: %w", err)
	}
	return res.LastInsertId()
}

// LoadSummaries returns all summary nodes for a session at a given level.
// If level <= 0, returns all levels. Ordered by msg_start ascending.
func (s *Store) LoadSummaries(sessionKey string, level int) ([]SummaryNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rows *sql.Rows
	var err error
	if level <= 0 {
		rows, err = s.db.Query(
			`SELECT id, session_key, level, content, token_est, created_at, msg_start, msg_end, parent_id
			 FROM summary_nodes WHERE session_key = ?
			 ORDER BY msg_start ASC`, sessionKey,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, session_key, level, content, token_est, created_at, msg_start, msg_end, parent_id
			 FROM summary_nodes WHERE session_key = ? AND level = ?
			 ORDER BY msg_start ASC`, sessionKey, level,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("lcm: load summaries: %w", err)
	}
	defer rows.Close()

	var nodes []SummaryNode
	for rows.Next() {
		var n SummaryNode
		if err := rows.Scan(&n.ID, &n.SessionKey, &n.Level, &n.Content,
			&n.TokenEst, &n.CreatedAt, &n.MsgStart, &n.MsgEnd, &n.ParentID); err != nil {
			return nil, fmt.Errorf("lcm: scan summary: %w", err)
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// LatestSummaryCoverage returns the highest msg_end covered by any summary
// for the given session. Returns -1 if no summaries exist.
func (s *Store) LatestSummaryCoverage(sessionKey string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var maxEnd sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(msg_end) FROM summary_nodes WHERE session_key = ?`, sessionKey,
	).Scan(&maxEnd)
	if err != nil || !maxEnd.Valid {
		return -1, err
	}
	return int(maxEnd.Int64), nil
}

// MaxMsgIndex returns the highest msg_index for a session. Returns -1 if empty.
func (s *Store) MaxMsgIndex(sessionKey string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var maxIdx sql.NullInt64
	err := s.db.QueryRow(
		`SELECT MAX(msg_index) FROM messages WHERE session_key = ?`, sessionKey,
	).Scan(&maxIdx)
	if err != nil || !maxIdx.Valid {
		return -1, err
	}
	return int(maxIdx.Int64), nil
}

// DeleteSession removes all messages and summaries for a session.
func (s *Store) DeleteSession(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("lcm: begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM summary_nodes WHERE session_key = ?`, sessionKey); err != nil {
		return fmt.Errorf("lcm: delete summaries: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_key = ?`, sessionKey); err != nil {
		return fmt.Errorf("lcm: delete messages: %w", err)
	}
	return tx.Commit()
}
