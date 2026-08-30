// Package mailwork tracks the local workflow state of each archived mail.
//
// Gmail labels answer "where is the message?". Deneb-native mail also needs to
// answer "what has the assistant done with it?" so the app can show analysis,
// feed, calendar-proposal, and to-do state without re-running an LLM or scraping
// chat history.
package mailwork

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

const (
	AnalysisQueued    = "queued"
	AnalysisAnalyzing = "analyzing"
	AnalysisDone      = "done"
	AnalysisFailed    = "failed"
	AnalysisStale     = "stale"
	AnalysisReview    = "review"

	FeedCreated = "created"
	FeedFailed  = "failed"
)

const maxLastErrorChars = 500

// MessageInput carries stable archive metadata into workflow state updates.
type MessageInput struct {
	ID              string
	ThreadID        string
	From            string
	Subject         string
	Date            string
	Mailbox         string
	HasAttachment   bool
	AttachmentCount int
}

// AnalysisInput records a completed analysis and its derived-item counts.
type AnalysisInput struct {
	MessageInput
	Quality            string
	DerivedCountsKnown bool
	// CalendarProposalCount and TodoCount are exact counts for the current
	// analysis only when DerivedCountsKnown is true. Cache hydration paths do
	// not have this information and must preserve existing downstream state.
	CalendarProposalCount int
	TodoCount             int
	DurationMs            int64
}

// MessageState is the durable assistant workflow state for one message.
type MessageState struct {
	ID                    string `json:"id"`
	ThreadID              string `json:"threadId,omitempty"`
	From                  string `json:"from,omitempty"`
	Subject               string `json:"subject,omitempty"`
	Date                  string `json:"date,omitempty"`
	Mailbox               string `json:"mailbox,omitempty"`
	HasAttachment         bool   `json:"hasAttachment,omitempty"`
	AttachmentCount       int    `json:"attachmentCount,omitempty"`
	AnalysisStatus        string `json:"analysisStatus,omitempty"`
	AnalysisQuality       string `json:"analysisQuality,omitempty"`
	AnalysisDurationMs    int64  `json:"analysisDurationMs,omitempty"`
	AnalysisUpdatedAtMs   int64  `json:"analysisUpdatedAtMs,omitempty"`
	ReviewReason          string `json:"reviewReason,omitempty"`
	FeedStatus            string `json:"feedStatus,omitempty"`
	FeedUpdatedAtMs       int64  `json:"feedUpdatedAtMs,omitempty"`
	CalendarProposalCount int    `json:"calendarProposalCount,omitempty"`
	TodoCount             int    `json:"todoCount,omitempty"`
	LastError             string `json:"lastError,omitempty"`
	LastSeenAtMs          int64  `json:"lastSeenAtMs,omitempty"`
	CreatedAtMs           int64  `json:"createdAtMs,omitempty"`
	UpdatedAtMs           int64  `json:"updatedAtMs,omitempty"`
}

// Summary aggregates workflow progress across all known messages.
type Summary struct {
	Messages           int   `json:"messages"`
	Analyzed           int   `json:"analyzed"`
	Analyzing          int   `json:"analyzing"`
	Failed             int   `json:"failed"`
	FeedCreated        int   `json:"feedCreated"`
	FeedMissing        int   `json:"feedMissing"`
	CalendarCandidates int   `json:"calendarCandidates"`
	TodoCandidates     int   `json:"todoCandidates"`
	UpdatedAtMs        int64 `json:"updatedAtMs,omitempty"`
}

type diskModel struct {
	Messages map[string]MessageState `json:"messages"`
}

// Store persists message workflow state in one atomically replaced JSON file.
type Store struct {
	path string
	mu   sync.Mutex
}

var (
	pathLocksMu sync.Mutex
	pathLocks   = map[string]*sync.Mutex{}
)

// New creates a workflow store backed by path.
func New(path string) *Store {
	return &Store{path: path}
}

// Get returns the current state for id, or the zero value on miss or read error.
func (s *Store) Get(id string) MessageState {
	if s == nil || strings.TrimSpace(id) == "" {
		return MessageState{}
	}
	unlock := s.lock()
	defer unlock()
	st, err := s.loadLocked()
	if err != nil {
		return MessageState{}
	}
	return st.Messages[strings.TrimSpace(id)]
}

// Summary returns aggregate state and suppresses storage errors.
func (s *Store) Summary() Summary {
	summary, _ := s.SummaryWithError()
	return summary
}

// SummaryWithError returns aggregate state and surfaces storage errors.
func (s *Store) SummaryWithError() (Summary, error) {
	if s == nil {
		return Summary{}, nil
	}
	unlock := s.lock()
	defer unlock()
	st, err := s.loadLocked()
	if err != nil {
		return Summary{}, err
	}
	return summarize(st.Messages), nil
}

// RememberMessage upserts archive metadata and refreshes the last-seen time.
func (s *Store) RememberMessage(in MessageInput) (MessageState, error) {
	if s == nil {
		return MessageState{}, nil
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		return MessageState{}, nil
	}
	now := time.Now().UnixMilli()
	return s.update(id, func(ms MessageState) MessageState {
		if ms.ID == "" {
			ms.ID = id
			ms.CreatedAtMs = now
		}
		mergeMessageInput(&ms, in)
		ms.LastSeenAtMs = now
		return ms
	})
}

// MarkAnalysisAnalyzing records that analysis has started.
func (s *Store) MarkAnalysisAnalyzing(in MessageInput) (MessageState, error) {
	return s.markAnalysis(in, AnalysisAnalyzing, "", nil)
}

// MarkAnalysisReview records a metadata-only item that autonomous analysis did
// not trust. Manual analysis may transition it through analyzing to done.
func (s *Store) MarkAnalysisReview(in MessageInput, reason string) (MessageState, error) {
	return s.markAnalysis(in, AnalysisReview, "", func(ms *MessageState) {
		ms.ReviewReason = truncateError(reason)
	})
}

// MarkAnalysisDone records a successful analysis and known derived counts.
func (s *Store) MarkAnalysisDone(in AnalysisInput) (MessageState, error) {
	if strings.TrimSpace(in.ID) == "" {
		return MessageState{}, nil
	}
	now := time.Now().UnixMilli()
	return s.update(strings.TrimSpace(in.ID), func(ms MessageState) MessageState {
		if ms.ID == "" {
			ms.ID = strings.TrimSpace(in.ID)
			ms.CreatedAtMs = now
		}
		mergeMessageInput(&ms, in.MessageInput)
		ms.AnalysisStatus = AnalysisDone
		ms.AnalysisQuality = strings.TrimSpace(in.Quality)
		ms.AnalysisDurationMs = maxInt64(0, in.DurationMs)
		ms.AnalysisUpdatedAtMs = now
		ms.ReviewReason = ""
		if in.DerivedCountsKnown {
			ms.CalendarProposalCount = nonNegativeInt(in.CalendarProposalCount)
			ms.TodoCount = nonNegativeInt(in.TodoCount)
		}
		ms.LastError = ""
		ms.UpdatedAtMs = now
		return ms
	})
}

// PendingAnalysis returns messages that were registered but never analyzed, or
// whose analysis failed, oldest registration first and bounded by limit.
//
// This query is why MarkAnalysisFailed means anything. Until 2026-08-30 the
// analysis sink wrote "failed" with a comment saying a later pass would
// re-analyze the message, and no such pass existed — the only reader of the
// status was a display filter in the Gmail row listing. The 60-day-old cohort
// in the live store was 41.7% never-analyzed (96 of 230), 87 of them
// human-sent, and nothing in the system was looking.
//
// Messages younger than minAge are excluded so a backfill never races the live
// poller for a mail that is simply still in flight.
func (s *Store) PendingAnalysis(limit int, minAge time.Duration) []MessageState {
	if limit <= 0 {
		return nil
	}
	unlock := s.lock()
	defer unlock()
	st, err := s.loadLocked()
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-minAge).UnixMilli()
	out := make([]MessageState, 0, limit)
	for _, ms := range st.Messages {
		switch ms.AnalysisStatus {
		case "", AnalysisFailed:
		default:
			continue
		}
		if ms.CreatedAtMs == 0 || ms.CreatedAtMs > cutoff {
			continue
		}
		out = append(out, ms)
	}
	// Least-recently-ATTEMPTED first, not oldest-registered first. A message
	// that can never produce a usable analysis would otherwise sit at the head
	// of an oldest-first queue and consume the bounded batch on every cycle,
	// starving everything behind it — the failure keeps its slot precisely
	// because it keeps failing. Ordering by the last attempt sends it to the
	// back each time, so the queue always makes progress.
	sort.Slice(out, func(a, b int) bool {
		return lastAttemptAt(out[a]) < lastAttemptAt(out[b])
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// lastAttemptAt reports when analysis last ran for a message; never-attempted
// messages report their registration time, which is always older.
func lastAttemptAt(ms MessageState) int64 {
	if ms.AnalysisUpdatedAtMs > 0 {
		return ms.AnalysisUpdatedAtMs
	}
	return ms.CreatedAtMs
}

// MarkAnalysisFailed records a bounded diagnostic for a failed analysis.
func (s *Store) MarkAnalysisFailed(in MessageInput, err error) (MessageState, error) {
	return s.markAnalysis(in, AnalysisFailed, errorText(err), nil)
}

// MarkFeedCreated records that the message produced a work-feed item.
func (s *Store) MarkFeedCreated(id string) (MessageState, error) {
	return s.markFeed(id, FeedCreated, nil)
}

// MarkDerivedCounts updates the calendar and to-do counts for id.
func (s *Store) MarkDerivedCounts(id string, calendarProposalCount, todoCount int) (MessageState, error) {
	if s == nil || strings.TrimSpace(id) == "" {
		return MessageState{}, nil
	}
	now := time.Now().UnixMilli()
	return s.update(strings.TrimSpace(id), func(ms MessageState) MessageState {
		if ms.ID == "" {
			ms.ID = strings.TrimSpace(id)
			ms.CreatedAtMs = now
		}
		ms.CalendarProposalCount = nonNegativeInt(calendarProposalCount)
		ms.TodoCount = nonNegativeInt(todoCount)
		ms.UpdatedAtMs = now
		return ms
	})
}

func (s *Store) markAnalysis(in MessageInput, status, lastError string, extra func(*MessageState)) (MessageState, error) {
	if s == nil || strings.TrimSpace(in.ID) == "" {
		return MessageState{}, nil
	}
	now := time.Now().UnixMilli()
	return s.update(strings.TrimSpace(in.ID), func(ms MessageState) MessageState {
		if ms.ID == "" {
			ms.ID = strings.TrimSpace(in.ID)
			ms.CreatedAtMs = now
		}
		mergeMessageInput(&ms, in)
		ms.AnalysisStatus = status
		ms.AnalysisUpdatedAtMs = now
		ms.LastError = truncateError(lastError)
		if status != AnalysisReview {
			ms.ReviewReason = ""
		}
		if extra != nil {
			extra(&ms)
		}
		ms.UpdatedAtMs = now
		return ms
	})
}

// Hint returns the operator-facing explanation for the current state without
// overloading review decisions as analysis errors.
func (ms MessageState) Hint() string {
	if ms.AnalysisStatus == AnalysisReview && strings.TrimSpace(ms.ReviewReason) != "" {
		return ms.ReviewReason
	}
	return ms.LastError
}

func (s *Store) markFeed(id, status string, err error) (MessageState, error) {
	if s == nil || strings.TrimSpace(id) == "" {
		return MessageState{}, nil
	}
	now := time.Now().UnixMilli()
	return s.update(strings.TrimSpace(id), func(ms MessageState) MessageState {
		if ms.ID == "" {
			ms.ID = strings.TrimSpace(id)
			ms.CreatedAtMs = now
		}
		ms.FeedStatus = status
		ms.FeedUpdatedAtMs = now
		if err != nil {
			ms.LastError = truncateError(err.Error())
		} else if status == FeedCreated {
			ms.LastError = ""
		}
		ms.UpdatedAtMs = now
		return ms
	})
}

func (s *Store) update(id string, mutate func(MessageState) MessageState) (MessageState, error) {
	if s == nil || id == "" {
		return MessageState{}, nil
	}
	unlock := s.lock()
	defer unlock()
	st, err := s.loadLocked()
	if err != nil {
		return MessageState{}, err
	}
	ms := mutate(st.Messages[id])
	if ms.ID == "" {
		ms.ID = id
	}
	if ms.UpdatedAtMs == 0 {
		ms.UpdatedAtMs = time.Now().UnixMilli()
	}
	st.Messages[id] = ms
	if err := s.saveLocked(st); err != nil {
		return MessageState{}, err
	}
	return ms, nil
}

func (s *Store) lock() func() {
	if s == nil {
		return func() {}
	}
	key := storeLockKey(s.path)
	if key == "" {
		s.mu.Lock()
		return s.mu.Unlock
	}
	pathLocksMu.Lock()
	l := pathLocks[key]
	if l == nil {
		l = &sync.Mutex{}
		pathLocks[key] = l
	}
	pathLocksMu.Unlock()
	l.Lock()
	return l.Unlock
}

func storeLockKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func (s *Store) loadLocked() (diskModel, error) {
	st := diskModel{Messages: map[string]MessageState{}}
	if s == nil || s.path == "" {
		return st, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return diskModel{Messages: map[string]MessageState{}}, err
	}
	if st.Messages == nil {
		st.Messages = map[string]MessageState{}
	}
	return st, nil
}

func (s *Store) saveLocked(st diskModel) error {
	if s == nil || s.path == "" {
		return nil
	}
	if st.Messages == nil {
		st.Messages = map[string]MessageState{}
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicfile.WriteFile(s.path, data, &atomicfile.Options{Perm: 0o600, DirPerm: 0o700})
}

func mergeMessageInput(ms *MessageState, in MessageInput) {
	if ms == nil {
		return
	}
	if v := strings.TrimSpace(in.ThreadID); v != "" {
		ms.ThreadID = v
	}
	if v := strings.TrimSpace(in.From); v != "" {
		ms.From = v
	}
	if v := strings.TrimSpace(in.Subject); v != "" {
		ms.Subject = v
	}
	if v := strings.TrimSpace(in.Date); v != "" {
		ms.Date = v
	}
	if v := strings.TrimSpace(in.Mailbox); v != "" {
		ms.Mailbox = v
	}
	ms.HasAttachment = ms.HasAttachment || in.HasAttachment || in.AttachmentCount > 0
	if in.AttachmentCount > ms.AttachmentCount {
		ms.AttachmentCount = in.AttachmentCount
	}
}

func summarize(messages map[string]MessageState) Summary {
	var out Summary
	for _, ms := range messages {
		out.Messages++
		out.UpdatedAtMs = maxInt64(out.UpdatedAtMs, ms.UpdatedAtMs)
		switch ms.AnalysisStatus {
		case AnalysisDone:
			out.Analyzed++
			if ms.FeedStatus != FeedCreated {
				out.FeedMissing++
			}
		case AnalysisAnalyzing, AnalysisQueued:
			out.Analyzing++
		case AnalysisFailed:
			out.Failed++
		}
		if ms.FeedStatus == FeedCreated {
			out.FeedCreated++
		}
		if ms.CalendarProposalCount > 0 {
			out.CalendarCandidates += ms.CalendarProposalCount
		}
		if ms.TodoCount > 0 {
			out.TodoCandidates += ms.TodoCount
		}
	}
	return out
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "context canceled"
	}
	return err.Error()
}

func truncateError(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= maxLastErrorChars {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLastErrorChars])
}

func maxInt64(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

func nonNegativeInt(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
