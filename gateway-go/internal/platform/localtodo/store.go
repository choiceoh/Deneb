// Package localtodo is the gateway's to-do store: lightweight tasks the user
// adds on the native client, persisted to {stateDir}/todos.json. It is the
// task-list companion to localcal (the local calendar): a to-do is a checkable
// item with an optional due date, while a calendar event is a time block. They
// are kept as separate stores/files because their shapes and lifecycles differ
// (a to-do has done/undone state and may have no date at all).
//
// Single-user, single-writer; a process-wide RWMutex suffices.
package localtodo

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
)

// IDPrefix tags to-do IDs so they never collide with calendar event IDs.
const IDPrefix = "todo:"

// IsTodoID reports whether id refers to a stored to-do.
func IsTodoID(id string) bool { return strings.HasPrefix(id, IDPrefix) }

// ErrNotFound is returned by Get/Update/Delete/SetDone when no to-do matches.
var ErrNotFound = errors.New("localtodo: todo not found")

// Todo is the in-memory shape a handler receives. Due is the zero time when the
// to-do has no due date; DueAllDay marks a whole-day due (time-of-day ignored).
type Todo struct {
	ID        string
	Title     string
	Note      string
	Due       time.Time
	DueAllDay bool
	Done      bool
	DoneAt    time.Time
	Created   time.Time
	Updated   time.Time
	// Source is an optional provenance key for automated creators (e.g.
	// "mail:<id>|<title>"). It is the dedup key for CreateIfAbsent and is not
	// exposed over the Mini App wire surface — purely internal bookkeeping.
	Source string
}

// CreateInput is the user-settable subset of a to-do. Due is optional (zero =
// no due date). Done is not set here — new to-dos start incomplete; completion
// flips through SetDone so the DoneAt stamp stays server-authoritative.
type CreateInput struct {
	Title     string
	Note      string
	Due       time.Time
	DueAllDay bool
	// Source is an optional provenance/dedup key (see Todo.Source). Empty for
	// user-created to-dos; set by automated creators that want idempotency.
	Source string
}

// storedTodo is the on-disk shape. Times are RFC3339 strings so the file stays
// human-readable and stable across restarts.
type storedTodo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Note      string `json:"note,omitempty"`
	Due       string `json:"due,omitempty"` // RFC3339, "" = no due date
	DueAllDay bool   `json:"dueAllDay,omitempty"`
	Done      bool   `json:"done,omitempty"`
	DoneAt    string `json:"doneAt,omitempty"` // RFC3339
	Created   string `json:"created,omitempty"`
	Updated   string `json:"updated,omitempty"`
	Source    string `json:"source,omitempty"`
}

func parseRFC3339(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func (t storedTodo) toTodo() Todo {
	return Todo{
		ID:        t.ID,
		Title:     t.Title,
		Note:      t.Note,
		Due:       parseRFC3339(t.Due),
		DueAllDay: t.DueAllDay,
		Done:      t.Done,
		DoneAt:    parseRFC3339(t.DoneAt),
		Created:   parseRFC3339(t.Created),
		Updated:   parseRFC3339(t.Updated),
		Source:    t.Source,
	}
}

// Store holds the locally-authored to-dos.
type Store struct {
	mu    sync.RWMutex
	path  string
	todos []storedTodo
	seq   int64 // monotonic so two creates in the same nanosecond get distinct IDs
}

var (
	globalMu    sync.Mutex
	globalStore *Store
)

// Default returns the process-wide store at {stateDir}/todos.json, mirroring
// localcal.Default: a failed init (corrupt file) is not cached, so a later call
// can retry once the file is fixed.
func Default() (*Store, error) {
	globalMu.Lock()
	defer globalMu.Unlock()
	if globalStore != nil {
		return globalStore, nil
	}
	s, err := New(filepath.Join(config.ResolveStateDir(), "todos.json"))
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
		return nil, fmt.Errorf("localtodo: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s.todos); err != nil {
		return nil, fmt.Errorf("localtodo: parse %s: %w", path, err)
	}
	return s, nil
}

// List returns all to-dos in display order: incomplete before complete, then by
// due date (dated before undated), then by creation time. The native list and
// the calendar day view both read from here.
func (s *Store) List() []Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Todo, 0, len(s.todos))
	for _, t := range s.todos {
		out = append(out, t.toTodo())
	}
	sortTodos(out)
	return out
}

// ListRange returns to-dos whose due date falls within [from, to), sorted in the
// same display order as List. Undated to-dos are never in a range. Used by the
// calendar to surface to-dos due on a given day.
func (s *Store) ListRange(from, to time.Time) []Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Todo, 0, len(s.todos))
	for _, st := range s.todos {
		t := st.toTodo()
		if t.Due.IsZero() {
			continue
		}
		if !t.Due.Before(from) && t.Due.Before(to) {
			out = append(out, t)
		}
	}
	sortTodos(out)
	return out
}

// Get returns the to-do with id, or nil when absent.
func (s *Store) Get(id string) *Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.todos {
		if t.ID == id {
			td := t.toTodo()
			return &td
		}
	}
	return nil
}

// Create appends a new to-do and persists. Returns the stored to-do.
func (s *Store) Create(in CreateInput) (Todo, error) {
	if err := validate(in); err != nil {
		return Todo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.newRecordLocked(in)
	s.todos = append(s.todos, rec)
	if err := s.persistLocked(); err != nil {
		return Todo{}, err
	}
	return rec.toTodo(), nil
}

// CreateIfAbsent creates the to-do only when no existing to-do shares the same
// non-empty Source key, making automated creators (e.g. the mail→todo sink)
// idempotent across re-analysis of the same source. On a match it returns the
// existing to-do with created=false and writes nothing. An empty Source
// disables the check and always creates, matching Create. The check + append
// happen under one lock so concurrent callers can't race a duplicate in.
func (s *Store) CreateIfAbsent(in CreateInput) (td Todo, created bool, err error) {
	if verr := validate(in); verr != nil {
		return Todo{}, false, verr
	}
	src := strings.TrimSpace(in.Source)
	s.mu.Lock()
	defer s.mu.Unlock()
	if src != "" {
		for _, t := range s.todos {
			if t.Source == src {
				return t.toTodo(), false, nil
			}
		}
	}
	rec := s.newRecordLocked(in)
	s.todos = append(s.todos, rec)
	if perr := s.persistLocked(); perr != nil {
		return Todo{}, false, perr
	}
	return rec.toTodo(), true, nil
}

// Update replaces the editable fields of the to-do with id (preserving its
// Created stamp and Done/DoneAt state) and persists.
func (s *Store) Update(id string, in CreateInput) (*Todo, error) {
	if err := validate(in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.todos {
		if s.todos[i].ID != id {
			continue
		}
		prev := s.todos[i]
		rec := buildRecord(id, in)
		rec.Created = prev.Created
		rec.Done = prev.Done
		rec.DoneAt = prev.DoneAt
		s.todos[i] = rec
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		td := rec.toTodo()
		return &td, nil
	}
	return nil, ErrNotFound
}

// SetDone flips the completion state of the to-do with id and persists. Marking
// done stamps DoneAt; un-marking clears it.
func (s *Store) SetDone(id string, done bool) (*Todo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.todos {
		if s.todos[i].ID != id {
			continue
		}
		now := time.Now().UTC()
		s.todos[i].Done = done
		if done {
			s.todos[i].DoneAt = now.Format(time.RFC3339)
		} else {
			s.todos[i].DoneAt = ""
		}
		s.todos[i].Updated = now.Format(time.RFC3339)
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
		td := s.todos[i].toTodo()
		return &td, nil
	}
	return nil, ErrNotFound
}

// Delete removes the to-do with id and persists.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.todos {
		if s.todos[i].ID != id {
			continue
		}
		s.todos = append(s.todos[:i], s.todos[i+1:]...)
		return s.persistLocked()
	}
	return ErrNotFound
}

func validate(in CreateInput) error {
	if strings.TrimSpace(in.Title) == "" {
		return fmt.Errorf("할 일 제목이 필요합니다") //nolint:staticcheck // ST1005 — Korean error message
	}
	return nil
}

// sortTodos orders to-dos for display: incomplete first, then earliest due
// (undated last), then oldest first.
func sortTodos(out []Todo) {
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Done != b.Done {
			return !a.Done // incomplete first
		}
		ad, bd := a.Due.IsZero(), b.Due.IsZero()
		if ad != bd {
			return !ad // dated before undated
		}
		if !ad && !a.Due.Equal(b.Due) {
			return a.Due.Before(b.Due)
		}
		return a.Created.Before(b.Created)
	})
}

// newRecordLocked builds a record with a fresh ID (mu held — uses s.seq).
func (s *Store) newRecordLocked(in CreateInput) storedTodo {
	s.seq++
	id := fmt.Sprintf("%s%d-%d", IDPrefix, time.Now().UnixNano(), s.seq)
	return buildRecord(id, in)
}

// buildRecord builds a stored record from input. Done state is set by the caller
// (Create starts incomplete; Update preserves prior state).
func buildRecord(id string, in CreateInput) storedTodo {
	now := time.Now().UTC().Format(time.RFC3339)
	due := ""
	if !in.Due.IsZero() {
		due = in.Due.Format(time.RFC3339)
	}
	return storedTodo{
		ID:        id,
		Title:     strings.TrimSpace(in.Title),
		Note:      strings.TrimSpace(in.Note),
		Due:       due,
		DueAllDay: in.DueAllDay,
		Created:   now,
		Updated:   now,
		Source:    strings.TrimSpace(in.Source),
	}
}

func (s *Store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("localtodo: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(s.todos, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // G306 — single-user host
		return fmt.Errorf("localtodo: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("localtodo: rename: %w", err)
	}
	return nil
}
