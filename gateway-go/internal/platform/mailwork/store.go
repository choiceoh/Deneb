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
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

const (
	AnalysisQueued    = "queued"
	AnalysisAnalyzing = "analyzing"
	AnalysisDone      = "done"
	AnalysisFailed    = "failed"
	AnalysisStale     = "stale"

	FeedCreated = "created"
	FeedFailed  = "failed"
)

const maxLastErrorChars = 500

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
	FeedStatus            string `json:"feedStatus,omitempty"`
	FeedUpdatedAtMs       int64  `json:"feedUpdatedAtMs,omitempty"`
	CalendarProposalCount int    `json:"calendarProposalCount,omitempty"`
	TodoCount             int    `json:"todoCount,omitempty"`
	LastError             string `json:"lastError,omitempty"`
	LastSeenAtMs          int64  `json:"lastSeenAtMs,omitempty"`
	CreatedAtMs           int64  `json:"createdAtMs,omitempty"`
	UpdatedAtMs           int64  `json:"updatedAtMs,omitempty"`
}

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

type Store struct {
	path string
	mu   sync.Mutex
}

var (
	globalMu    sync.Mutex
	globalStore *Store
)

func Default() *Store {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalStore == nil {
		globalStore = New(filepath.Join(config.ResolveStateDir(), "mail_work_state.json"))
	}
	return globalStore
}

func New(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Get(id string) MessageState {
	if s == nil || strings.TrimSpace(id) == "" {
		return MessageState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
	return st.Messages[strings.TrimSpace(id)]
}

func (s *Store) Snapshot() map[string]MessageState {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
	out := make(map[string]MessageState, len(st.Messages))
	for id, msg := range st.Messages {
		out[id] = msg
	}
	return out
}

func (s *Store) Summary() Summary {
	if s == nil {
		return Summary{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
	return summarize(st.Messages)
}

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

func (s *Store) MarkAnalysisQueued(in MessageInput) (MessageState, error) {
	return s.markAnalysis(in, AnalysisQueued, "", nil)
}

func (s *Store) MarkAnalysisAnalyzing(in MessageInput) (MessageState, error) {
	return s.markAnalysis(in, AnalysisAnalyzing, "", nil)
}

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
		ms.AnalysisDurationMs = in.DurationMs
		ms.AnalysisUpdatedAtMs = now
		if in.DerivedCountsKnown {
			ms.CalendarProposalCount = nonNegativeInt(in.CalendarProposalCount)
			ms.TodoCount = nonNegativeInt(in.TodoCount)
		}
		ms.LastError = ""
		ms.UpdatedAtMs = now
		return ms
	})
}

func (s *Store) MarkAnalysisFailed(in MessageInput, err error) (MessageState, error) {
	return s.markAnalysis(in, AnalysisFailed, errorText(err), nil)
}

func (s *Store) MarkAnalysisStale(id string) (MessageState, error) {
	return s.markAnalysis(MessageInput{ID: id}, AnalysisStale, "", nil)
}

func (s *Store) MarkFeedCreated(id string) (MessageState, error) {
	return s.markFeed(id, FeedCreated, nil)
}

func (s *Store) MarkFeedFailed(id string, err error) (MessageState, error) {
	return s.markFeed(id, FeedFailed, err)
}

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
		if extra != nil {
			extra(&ms)
		}
		ms.UpdatedAtMs = now
		return ms
	})
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
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.loadLocked()
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

func (s *Store) loadLocked() diskModel {
	st := diskModel{Messages: map[string]MessageState{}}
	if s == nil || s.path == "" {
		return st
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, &st)
	if st.Messages == nil {
		st.Messages = map[string]MessageState{}
	}
	return st
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
