package polaris

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
	"github.com/choiceoh/deneb/gateway-go/pkg/pathutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/textsearch"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Store is the file-backed immutable message store and summary DAG.
// Messages are stored as per-session JSONL files; summaries as per-session JSON snapshots.
type Store struct {
	dir           string
	mu            sync.Mutex // all methods need write access due to lazy session init
	sessions      map[string]*sessionData
	tokenEstimate func(string) int

	// summarySem is the optional dense-vector index over RESIDENT sessions'
	// summary nodes (semantic.go). nil until SetSummaryEmbedder; when present,
	// cross-session recall blends a meaning match against past-session summaries
	// with the keyword message search. Bounded to resident sessions, so it never
	// grows unbounded the way per-message vectors would.
	summarySem *embedindex.Index
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
// defaultPolarisLengthNorm keeps BM25's standard b. Lowering it for the
// transcript index was measured and REJECTED, which is worth recording because
// the arithmetic argued the other way: an assistant-authored answer runs 2.0x
// the corpus median length (147 vs 73 words on LongMemEval_s), so b=0.75 costs
// it ~43% of its score for length alone. The category does improve — its reach
// climbs 32.1% -> 35.7% — but overall retrieval falls monotonically, because
// every other category pays for the long documents that now rank higher
// (470 questions, full stack):
//
//	b     assistant hit@4   hit@8   hit@4   top1
//	0.75  30.4%             83.2%   81.3%   60.4%   <- current
//	0.4   32.1%             84.0%   81.1%   59.6%
//	0.2   32.1%             83.0%   80.2%   59.4%
//	0.0   32.1%             81.9%   79.4%   58.5%
//
// The length penalty is real; the cross-encoder already absorbs most of it, so
// there is nothing left to win in the retrieval stage. Re-sweep with
// DENEB_POLARIS_LENGTH_NORM only if the reranker is removed or bypassed.
const defaultPolarisLengthNorm = 0.75

// polarisLengthNorm is the BM25 length-normalization strength for the
// transcript index, overridable for sweeps via DENEB_POLARIS_LENGTH_NORM.
var polarisLengthNorm = polarisLengthNormFromEnv()

func polarisLengthNormFromEnv() float64 {
	if raw := strings.TrimSpace(os.Getenv("DENEB_POLARIS_LENGTH_NORM")); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 && v <= 1 {
			return v
		}
	}
	return defaultPolarisLengthNorm
}

func NewStore(path string) (*Store, error) {
	return NewStoreWithTokenEstimator(path, tokenest.Estimate)
}

// NewStoreWithTokenEstimator opens a store with an explicit token estimator.
// The Briefcase harness supplies the fixed, uncalibrated estimator so embedded
// runs cannot inherit process-global production calibration.
func NewStoreWithTokenEstimator(path string, estimate func(string) int) (*Store, error) {
	dir := path
	if strings.HasSuffix(path, ".db") {
		dir = filepath.Join(filepath.Dir(path), "polaris")
	}

	for _, sub := range []string{"messages", "summaries"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("polaris: mkdir: %w", err)
		}
	}

	if estimate == nil {
		estimate = tokenest.Estimate
	}
	s := &Store{
		dir:           dir,
		sessions:      make(map[string]*sessionData),
		tokenEstimate: estimate,
	}
	return s, nil
}

// Close is a no-op (files are written per mutation).
func (s *Store) Close() error {
	s.closeSummarySem()
	return nil
}

func (s *Store) messagesPath(sessionKey string) string {
	dir := pathutil.MustJoinUnder(s.dir, "messages")
	return pathutil.MustJoinUnder(dir, pathutil.SafeFileName(sessionKey)+".jsonl")
}

func (s *Store) summariesPath(sessionKey string) string {
	dir := pathutil.MustJoinUnder(s.dir, "summaries")
	return pathutil.MustJoinUnder(dir, pathutil.SafeFileName(sessionKey)+".json")
}

// ensureSession lazily loads a session's data into memory.
func (s *Store) ensureSession(sessionKey string) *sessionData {
	sd := s.sessions[sessionKey]
	if sd != nil {
		return sd
	}

	sd = &sessionData{
		// A transcript's document length tracks the KIND of message, not padding:
		// an assistant answer is long because answering takes words, a question
		// is short because asking does not. BM25's standard b=0.75 reads that as
		// evidence against the answer — measured on LongMemEval_s, the evidence
		// message for an assistant-authored answer runs 2.0x the corpus median
		// (147 vs 73 words), losing ~43% of its score for length alone, and that
		// category's retrieval sat far below every other one. See
		// textsearch.NewWithLengthNorm.
		fts: textsearch.NewWithLengthNorm(polarisLengthNorm),
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
			for i := range nodes {
				if nodes[i].Artifact == nil {
					nodes[i].Artifact = deriveConversationArtifact(nodes[i], sd.messages)
				}
			}
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
func (s *Store) AppendMessage(sessionKey string, msg chatport.ChatMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)

	content := string(msg.Content)
	textContent := indexableText(msg)
	tokenEst := s.tokenEstimate(textContent)
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

// indexableText renders a message for FTS indexing (and thus search snippets)
// as human-readable prose: text blocks, thinking prose, tool calls (name +
// input), and tool results. ChatMessage.TextContent falls back to the RAW JSON
// string for block arrays without text blocks — the common shape of tool-heavy
// assistant turns (thinking + tool_use only) — which indexed JSON syntax and
// made polaris search snippets read as `[{"type":"thinking",...` (production
// observation 2026-07-05). Plain-string content passes through unchanged.
func indexableText(msg chatport.ChatMessage) string {
	if len(msg.Content) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(msg.Content, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text,omitempty"`
		Thinking string          `json:"thinking,omitempty"`
		Name     string          `json:"name,omitempty"`
		Input    json.RawMessage `json:"input,omitempty"`
		Content  string          `json:"content,omitempty"`
	}
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return msg.TextContent()
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "thinking":
			if b.Thinking != "" {
				parts = append(parts, b.Thinking)
			}
		case "tool_use":
			if b.Name != "" {
				in := string(b.Input)
				if len(in) > 200 {
					in = textutil.TruncateBytes(in, 200)
				}
				parts = append(parts, "[도구 "+b.Name+"] "+in)
			}
		case "tool_result":
			if b.Content != "" {
				// Cap like tool_use inputs (but roomier): raw stdout/file dumps
				// would otherwise be duplicated into the FTS index + snippets,
				// bloating memory and exposing full sensitive payloads via
				// polaris search results.
				c := b.Content
				if len(c) > 2000 {
					c = textutil.TruncateBytes(c, 2000)
				}
				parts = append(parts, c)
			}
		}
	}
	if len(parts) == 0 {
		return msg.TextContent()
	}
	return strings.Join(parts, "\n")
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
func (s *Store) LoadMessages(sessionKey string, startIdx, endIdx int) ([]chatport.ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sd := s.ensureSession(sessionKey)

	var msgs []chatport.ChatMessage
	for _, m := range sd.messages {
		if m.MsgIndex < startIdx {
			continue
		}
		if endIdx >= 0 && m.MsgIndex > endIdx {
			continue
		}
		msgs = append(msgs, chatport.ChatMessage{
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
	if node.Artifact == nil {
		node.Artifact = deriveConversationArtifact(node, sd.messages)
	}
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
	// Wide is the 4x match window (textsearch.Hit.Wide) — model-pruning input,
	// never rendered directly.
	Wide string
	// NextText is the head of the FOLLOWING message when this hit is a user
	// turn answered by an assistant turn. A recall question phrased in the
	// user's vocabulary matches the user message; the fact it asks about lives
	// one message later — measured as the dominant failure of the
	// single-session-assistant category (reader stage: 43 of 47 failures had
	// the gold content absent from the block). Stitching the answer head onto
	// the hit lets the pruner keep it when it is the relevant part.
	NextText  string
	MsgIndex  int
	Timestamp int64
	Score     float64
}

// SearchMessages performs full-text search across message content.
// nextTextClipRunes bounds the assistant-reply head carried on a user-turn
// hit. 2400, up from 600: NextText is PRUNER INPUT, never rendered raw — the
// cross-encoder's sentence selection picks the rendered note from it (capped
// at noteCapFor), and the stitch fallback still clips to 300. At 600 the
// answer sentences of a long assistant reply (recommendation lists, drafts —
// the single-session-assistant shape) sat beyond the clip, so no sentence
// selector could ever surface them: R3 measured that raising the RENDER cap
// alone moved SSA 0.0 — the content was missing at the input, not the output.
const nextTextClipRunes = 2400

// assistantWide widens the pruner input for an assistant-authored hit: the 4x
// lexical window around the match often excludes the answer body of a long
// reply (the single-session-assistant failure shape), so the reply head joins
// it — head only when the window already sits inside the head.
func assistantWide(role, fullText, wide string) string {
	if role != "assistant" {
		return wide
	}
	head := clipRunesHead(fullText, nextTextClipRunes)
	if len(head) <= len(wide) {
		return wide
	}
	if strings.Contains(head, wide) {
		return head
	}
	return head + " … " + wide
}

// clipRunesHead returns the first n runes of text.
func clipRunesHead(text string, n int) string {
	r := []rune(strings.TrimSpace(text))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "..."
}

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
		// Find the message by index; skip hits with malformed IDs so a
		// parse failure cannot silently match message index 0.
		msgIdx, err := strconv.Atoi(h.ID)
		if err != nil {
			continue
		}
		for mi, m := range sd.messages {
			if m.MsgIndex == msgIdx {
				next := ""
				if m.Role == "user" && mi+1 < len(sd.messages) && sd.messages[mi+1].Role == "assistant" {
					next = clipRunesHead(sd.messages[mi+1].TextContent, nextTextClipRunes)
				}
				results = append(results, SearchHit{
					SessionKey: sessionKey,
					Role:       m.Role,
					Snippet:    h.Snippet,
					Wide:       assistantWide(m.Role, m.TextContent, h.Wide),
					NextText:   next,
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

// SearchResidentSessions runs query against the FTS index of every session
// currently resident in memory EXCEPT excludeKey, returning the best-scoring
// hits merged across them. It performs NO disk I/O — only sessions already
// loaded this uptime are searched — so it is a cheap, aux-LLM-free way to surface
// relevant messages from other recent conversations. This is Deneb's analogue of
// hermes-agent's cross-session session_search: today recall only sees the
// current session, leaving anything said in a different session invisible to the
// polaris path. Sessions that were never loaded (paged out on disk) are
// intentionally skipped to keep recall within its latency budget; wiki/diary
// remain the durable cross-session memory for older material.
func (s *Store) SearchResidentSessions(excludeKey, query string, maxResults int) ([]SearchHit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if query == "" || maxResults <= 0 {
		return nil, nil
	}

	var results []SearchHit
	for key, sd := range s.sessions {
		if key == excludeKey {
			continue
		}
		hits := sd.fts.Search(query, maxResults)
		for _, h := range hits {
			var msgIdx int
			if n, _ := fmt.Sscanf(h.ID, "%d", &msgIdx); n != 1 {
				continue
			}
			for mi, m := range sd.messages {
				if m.MsgIndex == msgIdx {
					next := ""
					if m.Role == "user" && mi+1 < len(sd.messages) && sd.messages[mi+1].Role == "assistant" {
						next = clipRunesHead(sd.messages[mi+1].TextContent, nextTextClipRunes)
					}
					results = append(results, SearchHit{
						SessionKey: key,
						Role:       m.Role,
						Snippet:    h.Snippet,
						Wide:       assistantWide(m.Role, m.TextContent, h.Wide),
						NextText:   next,
						MsgIndex:   m.MsgIndex,
						Timestamp:  m.Timestamp,
						Score:      h.Score / (h.Score + 1), // normalize to 0-1
					})
					break
				}
			}
		}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results, nil
}

// RecentSummariesAcrossSessions returns up to limit most recent summary nodes
// across all sessions, sorted by CreatedAt descending. Used by wiki dreamer
// to seed fact synthesis with polaris-compressed conversation history.
func (s *Store) RecentSummariesAcrossSessions(limit int) []SummaryNode {
	s.mu.Lock()
	defer s.mu.Unlock()

	var all []SummaryNode
	for _, sd := range s.sessions {
		all = append(all, sd.summaries...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt > all[j].CreatedAt })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
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
	var deleteErrs []error
	if err := os.Remove(s.messagesPath(sessionKey)); err != nil && !errors.Is(err, os.ErrNotExist) {
		deleteErrs = append(deleteErrs, fmt.Errorf("polaris: delete messages: %w", err))
	}
	if err := os.Remove(s.summariesPath(sessionKey)); err != nil && !errors.Is(err, os.ErrNotExist) {
		deleteErrs = append(deleteErrs, fmt.Errorf("polaris: delete summaries: %w", err))
	}
	return errors.Join(deleteErrs...)
}
