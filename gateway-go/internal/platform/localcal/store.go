// Package localcal is the gateway's own calendar store: events the user adds by
// hand on the native client, persisted to {stateDir}/calendar.json. It exists so
// the calendar is fully functional (create/edit/delete) without depending on a
// Google OAuth write scope — reads in the handler merge these with the read-only
// Google calendar, while writes always land here.
//
// Single-user, single-writer; a process-wide RWMutex suffices.
package localcal

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// IDPrefix tags locally-created events so the handler can route get/update/delete
// to this store instead of Google. Google event IDs never start with this.
const IDPrefix = "local:"

// IsLocalID reports whether id refers to a locally-stored event.
func IsLocalID(id string) bool { return strings.HasPrefix(id, IDPrefix) }

// ErrNotFound is returned by Get/Update/Delete when no local event matches.
var ErrNotFound = errors.New("localcal: event not found")

// CreateInput is the user-settable subset of an event. Times are absolute
// instants (parsed from the client's RFC3339); AllDay marks a whole-day event.
type CreateInput struct {
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	AllDay      bool

	// Deneb provenance — set when an event is generated from analysis (a mail
	// proposal, a deal due date). Source is a machine link ("mail:<msgID>"),
	// SourceLabel a human one (the mail subject), Kind the type ("meeting" |
	// "deadline"). All empty for a plain hand-added event.
	Source      string
	SourceLabel string
	Kind        string
	// Docs lists the originating mail's document attachments (견적서/계약서 등
	// filenames) so meeting prep can pull the actual documents, not just the mail
	// text. Empty for a plain event.
	Docs []string
}

// storedEvent is the on-disk shape. Times are RFC3339 strings so the file stays
// human-readable and stable across restarts.
type storedEvent struct {
	ID          string   `json:"id"`
	Summary     string   `json:"summary"`
	Description string   `json:"description,omitempty"`
	Location    string   `json:"location,omitempty"`
	Start       string   `json:"start"` // RFC3339
	End         string   `json:"end"`   // RFC3339
	AllDay      bool     `json:"allDay,omitempty"`
	Source      string   `json:"source,omitempty"`
	SourceLabel string   `json:"sourceLabel,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	Docs        []string `json:"docs,omitempty"`
	Created     string   `json:"created,omitempty"`
	Updated     string   `json:"updated,omitempty"`
}

func (e storedEvent) toCalendar() calendar.Event {
	start, _ := time.Parse(time.RFC3339, e.Start)
	end, _ := time.Parse(time.RFC3339, e.End)
	return calendar.Event{
		ID:          e.ID,
		Summary:     e.Summary,
		Description: e.Description,
		Location:    e.Location,
		Start:       start,
		End:         end,
		AllDay:      e.AllDay,
		Status:      "confirmed",
		Source:      e.Source,
		SourceLabel: e.SourceLabel,
		Kind:        e.Kind,
		Docs:        e.Docs,
	}
}

// Store holds the locally-authored events.
type Store struct {
	mu     sync.RWMutex
	path   string
	events []storedEvent
	seq    int64 // monotonic so two creates in the same nanosecond get distinct IDs

	// onChange, when set via SetChangeObserver, is invoked (outside the store lock)
	// after each successful Create/Update/Delete. The server wires it to the
	// native-sync stream so the client's calendar cache refreshes promptly; the
	// platform store itself stays free of any nativesync dependency.
	onChange func(eventID string)
}

// SetChangeObserver registers a callback invoked once after each successful
// mutation. Single-user, single-writer: set once at wiring time, before the store
// is used concurrently.
func (s *Store) SetChangeObserver(fn func(eventID string)) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// notifyChange invokes the observer outside the store lock (callers release first)
// so the callback — which appends to native-sync (its own lock + a file write) —
// never runs under s.mu.
func (s *Store) notifyChange(eventID string) {
	s.mu.RLock()
	fn := s.onChange
	s.mu.RUnlock()
	if fn != nil {
		fn(eventID)
	}
}

var (
	globalMu    sync.Mutex
	globalStore *Store
)

// Default returns the process-wide store at {stateDir}/calendar.json, mirroring
// calendar.DefaultClient / gmail.DefaultClient: a failed init (corrupt file) is
// not cached, so a later call can retry once the file is fixed.
func Default() (*Store, error) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalStore != nil {
		return globalStore, nil
	}
	s, err := New(filepath.Join(config.ResolveStateDir(), "calendar.json"))
	if err != nil {
		return nil, err
	}
	globalStore = s
	return globalStore, nil
}

// New loads the store from path (an empty store if the file is absent).
func New(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("localcal: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s.events); err != nil {
		return nil, fmt.Errorf("localcal: parse %s: %w", path, err)
	}
	return s, nil
}

// ListRange returns events that overlap [from, to), sorted by start. An event
// overlaps when it starts before the window ends and ends after the window
// begins, so a multi-day event is returned for every month grid whose range it
// touches — not only the one containing its start day. An event with no usable
// end (zero, or not after start) is treated as an instant at Start.
func (s *Store) ListRange(from, to time.Time) []calendar.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]calendar.Event, 0, len(s.events))
	for _, e := range s.events {
		ev := e.toCalendar()
		if ev.Start.IsZero() {
			continue
		}
		end := ev.End
		if !end.After(ev.Start) {
			end = ev.Start
		}
		// Half-open overlap: [Start, end) intersects [from, to).
		if ev.Start.Before(to) && end.After(from) {
			out = append(out, ev)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// Get returns the event with id, or nil when absent.
func (s *Store) Get(id string) *calendar.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.events {
		if e.ID == id {
			ev := e.toCalendar()
			return &ev
		}
	}
	return nil
}

// Create appends a new event and persists. Returns the stored event.
func (s *Store) Create(in CreateInput) (calendar.Event, error) {
	if err := validate(in); err != nil {
		return calendar.Event{}, err
	}
	s.mu.Lock()
	rec := s.newRecordLocked(in)
	s.events = append(s.events, rec)
	err := s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		return calendar.Event{}, err
	}
	ev := rec.toCalendar()
	s.notifyChange(ev.ID)
	return ev, nil
}

// Update replaces the event with id (preserving its Created stamp and Deneb
// provenance) and persists.
func (s *Store) Update(id string, in CreateInput) (*calendar.Event, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	idx := -1
	for i := range s.events {
		if s.events[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	rec := buildRecord(id, in)
	rec.Created = s.events[idx].Created
	// Preserve the origin link the editor didn't re-supply: a user editing the
	// time/title of a proposal-accepted meeting must not lose which mail it came
	// from (the update callers only carry summary/time/location, never these).
	// An explicit value in `in` still overrides, per field.
	if rec.Source == "" {
		rec.Source = s.events[idx].Source
	}
	if rec.SourceLabel == "" {
		rec.SourceLabel = s.events[idx].SourceLabel
	}
	if rec.Kind == "" {
		rec.Kind = s.events[idx].Kind
	}
	if len(rec.Docs) == 0 {
		rec.Docs = s.events[idx].Docs
	}
	s.events[idx] = rec
	err := s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	ev := rec.toCalendar()
	s.notifyChange(ev.ID)
	return &ev, nil
}

// Delete removes the event with id and persists.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	idx := -1
	for i := range s.events {
		if s.events[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return ErrNotFound
	}
	s.events = append(s.events[:idx], s.events[idx+1:]...)
	err := s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	s.notifyChange(id)
	return nil
}

func validate(in CreateInput) error {
	if strings.TrimSpace(in.Summary) == "" {
		return fmt.Errorf("일정 제목이 필요합니다") //nolint:staticcheck // ST1005 — Korean error message
	}
	if in.Start.IsZero() {
		return fmt.Errorf("일정 시작 시각이 필요합니다") //nolint:staticcheck // ST1005 — Korean error message
	}
	return nil
}

// newRecordLocked builds a record with a fresh ID (mu held — uses s.seq).
func (s *Store) newRecordLocked(in CreateInput) storedEvent {
	s.seq++
	id := fmt.Sprintf("%s%d-%d", IDPrefix, time.Now().UnixNano(), s.seq)
	return buildRecord(id, in)
}

// buildRecord builds a stored record from input, applying a default end when
// none (or a non-after end) is given: +1 day for all-day, +1 hour otherwise.
func buildRecord(id string, in CreateInput) storedEvent {
	end := in.End
	if !end.After(in.Start) {
		if in.AllDay {
			end = in.Start.Add(24 * time.Hour)
		} else {
			end = in.Start.Add(time.Hour)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return storedEvent{
		ID:          id,
		Summary:     strings.TrimSpace(in.Summary),
		Description: strings.TrimSpace(in.Description),
		Location:    strings.TrimSpace(in.Location),
		Start:       in.Start.Format(time.RFC3339),
		End:         end.Format(time.RFC3339),
		AllDay:      in.AllDay,
		Source:      strings.TrimSpace(in.Source),
		SourceLabel: strings.TrimSpace(in.SourceLabel),
		Kind:        strings.TrimSpace(in.Kind),
		Docs:        in.Docs,
		Created:     now,
		Updated:     now,
	}
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("localcal: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(s.events, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // G306 — single-user host
		return fmt.Errorf("localcal: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("localcal: rename: %w", err)
	}
	return nil
}
