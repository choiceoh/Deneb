package polaris

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
	"github.com/choiceoh/deneb/gateway-go/pkg/textsearch"
)

// Store is the file-backed immutable message store and summary DAG.
// Messages are stored as per-session JSONL files; summaries as per-session JSON snapshots.
type Store struct {
	dir      string
	mu       sync.Mutex // all methods need write access due to lazy session init
	sessions map[string]*sessionData
}

// messageRecord is the on-disk JSONL format for a single message.
type messageRecord struct {
	Role        string          `json:"role"`
	Content     json.RawMessage `json:"content"`
	TextContent string          `json:"textContent"`
	Timestamp   int64           `json:"ts"`
	TokenEst    int             `json:"tokenEst"`
	MsgIndex    int             `json:"msgIndex"`
}

// sessionData holds the in-memory state for a single session.
type sessionData struct {
	messages     []messageRecord
	summaries    []SummaryNode
	nextMsgIndex int
	nextSumID    int64
	totalTokens  int
	fts          *textsearch.Index
}

// NewStore opens (or creates) the Polaris file store.
// The path parameter is reinterpreted as a base: if it ends in ".db",
// we strip that and use the parent directory + "polaris/" subdirectory.
func NewStore(path string) (*Store, error) {
	dir := path
	if strings.HasSuffix(path, ".db") {
		dir = filepath.Join(filepath.Dir(path), "polaris")
	}

	for _, sub := range []string{"messages", "summaries"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("polaris: mkdir: %w", err)
		}
	}

	s := &Store{
		dir:      dir,
		sessions: make(map[string]*sessionData),
	}
	return s, nil
}

// Close is a no-op (files are written per mutation).
func (s *Store) Close() error {
	return nil
}

func (s *Store) messagesPath(sessionKey string) string {
	return filepath.Join(s.dir, "messages", sessionKey+".jsonl")
}

func (s *Store) summariesPath(sessionKey string) string {
	return filepath.Join(s.dir, "summaries", sessionKey+".json")
}

// ensureSession lazily loads a session's data into memory.
func (s *Store) ensureSession(sessionKey string) *sessionData {
	sd := s.sessions[sessionKey]
	if sd != nil {
		return sd
	}

	sd = &sessionData{
		fts: textsearch.New(),
	}

	// Load messages from JSONL.
	msgs, _ := jsonlstore.Load[messageRecord](s.messagesPath(sessionKey))
	sd.messages = msgs
	for i := range msgs {
		m := &msgs[i]
		sd.totalTokens += m.TokenEst
		if m.MsgIndex >= sd.nextMsgIndex {
			sd.nextMsgIndex = m.MsgIndex + 1
		}
		// Index for FTS.
		sd.fts.Upsert(fmt.Sprintf("%d", m.MsgIndex), m.TextContent)
	}

	// Load summaries from JSON snapshot.
	data, err := os.ReadFile(s.summariesPath(sessionKey))
	if err == nil {
		var nodes []SummaryNode
		if json.Unmarshal(data, &nodes) == nil {
			sd.summaries = nodes
			for _, n := range nodes {
				if n.ID >= sd.nextSumID {
					sd.nextSumID = n.ID + 1
				}
			}
		}
	}

	s.sessions[sessionKey] = sd
	return sd
}

// AppendMessage inserts a ChatMessage into the immutable store.
// msg_index is auto-assigned as max(msg_index)+1 for the session.
func (s *Store) AppendMessage(sessionKey string, msg toolctx.ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)

	content := string(msg.Content)
	textContent := msg.TextContent()
	tokenEst := tokenest.Estimate(textContent)
	ts := msg.Timestamp
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}

	rec := messageRecord{
		Role:        msg.Role,
		Content:     json.RawMessage(content),
		TextContent: textContent,
		Timestamp:   ts,
		TokenEst:    tokenEst,
		MsgIndex:    sd.nextMsgIndex,
	}

	if err := jsonlstore.Append(s.messagesPath(sessionKey), rec); err != nil {
		return fmt.Errorf("polaris: append message: %w", err)
	}

	sd.messages = append(sd.messages, rec)
	sd.totalTokens += tokenEst
	sd.nextMsgIndex++
	sd.fts.Upsert(fmt.Sprintf("%d", rec.MsgIndex), textContent)

	return nil
}

// MessageCount returns the number of messages for a session.
func (s *Store) MessageCount(sessionKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)
	return len(sd.messages), nil
}

// SessionTokens returns the total estimated tokens for a session's messages.
func (s *Store) SessionTokens(sessionKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)
	return sd.totalTokens, nil
}

// LoadMessages returns messages in [startIdx, endIdx] range (inclusive).
// If endIdx < 0, loads from startIdx to the end.
func (s *Store) LoadMessages(sessionKey string, startIdx, endIdx int) ([]toolctx.ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)

	var msgs []toolctx.ChatMessage
	for _, m := range sd.messages {
		if m.MsgIndex < startIdx {
			continue
		}
		if endIdx >= 0 && m.MsgIndex > endIdx {
			continue
		}
		msgs = append(msgs, toolctx.ChatMessage{
			Role:      m.Role,
			Content:   json.RawMessage(m.Content),
			Timestamp: m.Timestamp,
		})
	}
	return msgs, nil
}

// MaxMsgIndex returns the highest msg_index for a session. Returns -1 if empty.
func (s *Store) MaxMsgIndex(sessionKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)
	if len(sd.messages) == 0 {
		return -1, nil
	}
	return sd.nextMsgIndex - 1, nil
}

// InsertSummary stores a summary node and returns its auto-generated ID.
func (s *Store) InsertSummary(node SummaryNode) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(node.SessionKey)
	node.ID = sd.nextSumID
	sd.nextSumID++
	sd.summaries = append(sd.summaries, node)

	if err := s.saveSummaries(node.SessionKey, sd); err != nil {
		return 0, fmt.Errorf("polaris: insert summary: %w", err)
	}
	return node.ID, nil
}

func (s *Store) saveSummaries(sessionKey string, sd *sessionData) error {
	data, err := json.Marshal(sd.summaries)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(s.summariesPath(sessionKey), data, &atomicfile.Options{Fsync: true})
}

// LoadSummaries returns all summary nodes for a session at a given level.
// If level <= 0, returns all levels. Ordered by msg_start ascending.
func (s *Store) LoadSummaries(sessionKey string, level int) ([]SummaryNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)

	var nodes []SummaryNode
	for _, n := range sd.summaries {
		if level > 0 && n.Level != level {
			continue
		}
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].MsgStart < nodes[j].MsgStart })
	return nodes, nil
}

// LatestSummaryCoverage returns the highest msg_end covered by any summary.
// Returns -1 if no summaries exist.
func (s *Store) LatestSummaryCoverage(sessionKey string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)
	maxEnd := -1
	for _, n := range sd.summaries {
		if n.MsgEnd > maxEnd {
			maxEnd = n.MsgEnd
		}
	}
	return maxEnd, nil
}

// SearchHit is a single FTS search result.
type SearchHit struct {
	SessionKey string
	Role       string
	Snippet    string
	MsgIndex   int
	Timestamp  int64
	Score      float64
}

// SearchMessages performs full-text search across message content.
func (s *Store) SearchMessages(sessionKey, query string, maxResults int) ([]SearchHit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if query == "" || maxResults <= 0 {
		return nil, nil
	}

	sd := s.ensureSession(sessionKey)
	hits := sd.fts.Search(query, maxResults)

	var results []SearchHit
	for _, h := range hits {
		// Find the message by index.
		var msgIdx int
		fmt.Sscanf(h.ID, "%d", &msgIdx)
		for _, m := range sd.messages {
			if m.MsgIndex == msgIdx {
				results = append(results, SearchHit{
					SessionKey: sessionKey,
					Role:       m.Role,
					Snippet:    h.Snippet,
					MsgIndex:   m.MsgIndex,
					Timestamp:  m.Timestamp,
					Score:      h.Score / (h.Score + 1), // normalize to 0-1
				})
				break
			}
		}
	}
	return results, nil
}

// SummaryByID loads a single summary node by its ID.
func (s *Store) SummaryByID(id int64) (*SummaryNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, sd := range s.sessions {
		for i := range sd.summaries {
			if sd.summaries[i].ID == id {
				n := sd.summaries[i]
				return &n, nil
			}
		}
	}
	return nil, fmt.Errorf("polaris: summary node %d not found", id)
}

// LoadUncondensedNodes returns summary nodes at the given level that have not
// been absorbed into a higher-level condensed node (parent_id IS NULL).
func (s *Store) LoadUncondensedNodes(sessionKey string, level int) ([]SummaryNode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)

	var nodes []SummaryNode
	for _, n := range sd.summaries {
		if n.Level == level && n.ParentID == nil {
			nodes = append(nodes, n)
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].MsgStart < nodes[j].MsgStart })
	return nodes, nil
}

// UpdateParentID marks nodes as absorbed by a condensed parent node.
func (s *Store) UpdateParentID(nodeIDs []int64, parentID int64) error {
	if len(nodeIDs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	idSet := make(map[int64]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		idSet[id] = true
	}

	// Find the session for these nodes and update them.
	for sessionKey, sd := range s.sessions {
		changed := false
		for i := range sd.summaries {
			if idSet[sd.summaries[i].ID] {
				pid := parentID
				sd.summaries[i].ParentID = &pid
				changed = true
			}
		}
		if changed {
			if err := s.saveSummaries(sessionKey, sd); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteSession removes all messages and summaries for a session.
func (s *Store) DeleteSession(sessionKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sessionKey)
	os.Remove(s.messagesPath(sessionKey))
	os.Remove(s.summariesPath(sessionKey))
	return nil
}
