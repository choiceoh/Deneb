package nativesync

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	TypeTranscriptAppended = "transcript.appended"
	TypeWorkFeedCreated    = "workfeed.created"
	TypeWorkFeedUpdated    = "workfeed.updated"
	TypeWorkFeedActionRun  = "workfeed.action.run"
)

var ErrInvalidEvent = errors.New("native sync event type is required")

type Event struct {
	Seq            int64           `json:"seq"`
	Type           string          `json:"type"`
	EntityID       string          `json:"entityId,omitempty"`
	SessionKey     string          `json:"sessionKey,omitempty"`
	WorkFeedItemID string          `json:"workFeedItemId,omitempty"`
	TimestampMs    int64           `json:"timestampMs"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

type AppendInput struct {
	Type           string
	EntityID       string
	SessionKey     string
	WorkFeedItemID string
	Payload        any
}

type PullResult struct {
	Events    []Event
	Cursor    int64
	LatestSeq int64
	HasMore   bool
}

type Store struct {
	path    string
	mu      sync.Mutex
	loaded  bool
	nextSeq int64
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Append(in AppendInput) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureLoadedLocked(); err != nil {
		return Event{}, err
	}
	typ := strings.TrimSpace(in.Type)
	if typ == "" {
		return Event{}, ErrInvalidEvent
	}
	payload, err := marshalPayload(in.Payload)
	if err != nil {
		return Event{}, err
	}
	ev := Event{
		Seq:            s.nextSeq,
		Type:           typ,
		EntityID:       strings.TrimSpace(in.EntityID),
		SessionKey:     strings.TrimSpace(in.SessionKey),
		WorkFeedItemID: strings.TrimSpace(in.WorkFeedItemID),
		TimestampMs:    time.Now().UnixMilli(),
		Payload:        payload,
	}
	if ev.Seq <= 0 {
		ev.Seq = 1
	}
	if err := jsonlstore.Append(s.path, ev); err != nil {
		return Event{}, err
	}
	s.nextSeq = ev.Seq + 1
	return ev, nil
}

func (s *Store) Pull(afterSeq int64, limit int) (PullResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events, err := s.loadSortedLocked()
	if err != nil {
		return PullResult{}, err
	}
	if afterSeq < 0 {
		afterSeq = 0
	}
	var latest int64
	for _, ev := range events {
		if ev.Seq > latest {
			latest = ev.Seq
		}
	}
	filtered := make([]Event, 0, len(events))
	for _, ev := range events {
		if ev.Seq > afterSeq {
			filtered = append(filtered, ev)
		}
	}
	hasMore := false
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
		hasMore = true
	}
	cursor := afterSeq
	if len(filtered) > 0 {
		cursor = filtered[len(filtered)-1].Seq
	}
	return PullResult{
		Events:    filtered,
		Cursor:    cursor,
		LatestSeq: latest,
		HasMore:   hasMore,
	}, nil
}

func (s *Store) ensureLoadedLocked() error {
	if s.loaded {
		return nil
	}
	events, err := s.loadSortedLocked()
	if err != nil {
		return err
	}
	var maxSeq int64
	for _, ev := range events {
		if ev.Seq > maxSeq {
			maxSeq = ev.Seq
		}
	}
	s.nextSeq = maxSeq + 1
	if s.nextSeq <= 0 {
		s.nextSeq = 1
	}
	s.loaded = true
	return nil
}

func (s *Store) loadSortedLocked() ([]Event, error) {
	events, err := jsonlstore.Load[Event](s.path)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(events))
	for _, ev := range events {
		ev.Type = strings.TrimSpace(ev.Type)
		if ev.Seq <= 0 || ev.Type == "" {
			continue
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Seq < out[j].Seq
	})
	return out, nil
}

func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return nil, nil
	}
	if raw, ok := payload.(json.RawMessage); ok {
		return raw, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return data, nil
}
