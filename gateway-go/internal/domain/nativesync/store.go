package nativesync

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Prune caps for native_sync.jsonl. This is the hottest append file (~1 line
// per assistant turn) and Pull re-reads + sorts the whole file on every client
// poll, so an uncapped file is both unbounded growth and O(n) per poll. Mirror
// agentlog's pruneIfNeeded thresholds (5 MB / last 3000 events): when the file
// exceeds maxLogBytes, rewrite it keeping only the last keepEvents.
const (
	maxLogBytes = 5_000_000 // 5 MB before a prune is triggered
	keepEvents  = 3_000     // events retained after a prune
)

const (
	TypeTranscriptAppended = "transcript.appended"
	TypeWorkFeedCreated    = "workfeed.created"
	TypeWorkFeedUpdated    = "workfeed.updated"
	TypeWorkFeedActionRun  = "workfeed.action.run"
	// TypeCalendarChanged signals a server-side local-calendar mutation (create/
	// edit/delete) so the client refetches its calendar promptly instead of waiting
	// out its background warm throttle. Carries only the event ID — the client
	// refreshes the whole upcoming list, so no per-field payload is needed.
	TypeCalendarChanged = "calendar.changed"
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
	s.pruneIfNeededLocked()
	return ev, nil
}

// pruneIfNeededLocked rewrites native_sync.jsonl keeping only the last
// keepEvents when the file grows past maxLogBytes. Caller must hold s.mu.
//
// Mirrors agentlog.pruneIfNeeded: a cheap os.Stat gate so the common append
// path stays O_APPEND-only, then an atomic rewrite (jsonlstore.Snapshot does
// the temp-write + rename) on the rare overflow. Prune failures are best-effort
// — a logged warning, never a failed Append: a file we couldn't shrink is still
// fully readable, so correctness is preserved even if growth isn't capped this
// round. Seq monotonicity is unaffected; nextSeq already advanced above and the
// retained tail keeps the highest seqs, so loadSortedLocked stays consistent.
func (s *Store) pruneIfNeededLocked() {
	stat, err := os.Stat(s.path)
	if err != nil || stat.Size() <= int64(maxLogBytes) {
		return
	}
	events, err := s.loadSortedLocked()
	if err != nil || len(events) <= keepEvents {
		return
	}
	kept := events[len(events)-keepEvents:]
	if err := jsonlstore.Snapshot(s.path, kept); err != nil {
		slog.Warn("nativesync: prune snapshot failed — file not rotated",
			"path", s.path, "error", err)
	}
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
