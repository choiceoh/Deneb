// Package prompts stores operator-editable prompt instructions.
package prompts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

const maxPromptRunes = 80_000

var (
	ErrNotFound = errors.New("prompt not found")
	ErrReadOnly = errors.New("prompt is read-only")
	ErrEmpty    = errors.New("prompt text is empty")
	ErrTooLarge = errors.New("prompt text is too large")
)

// Template defines a built-in prompt and its default text.
type Template struct {
	ID          string
	Title       string
	Description string
	Category    string
	DefaultText string
	Editable    bool
}

// Entry is the effective prompt plus override metadata returned to callers.
type Entry struct {
	ID          string
	Title       string
	Description string
	Category    string
	Text        string
	DefaultText string
	Editable    bool
	Overridden  bool
	UpdatedAtMs int64
}

type overrideFile struct {
	Prompts map[string]overrideEntry `json:"prompts"`
}

type overrideEntry struct {
	Text        string `json:"text"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
}

// Store manages built-in prompts and their durable operator overrides.
type Store struct {
	path      string
	byID      map[string]Template
	order     []string
	mu        sync.Mutex
	overrides map[string]overrideEntry
	loaded    bool
}

// NewStore constructs a prompt store backed by path.
func NewStore(path string, templates []Template) *Store {
	byID := make(map[string]Template, len(templates))
	order := make([]string, 0, len(templates))
	for _, t := range templates {
		t.ID = strings.TrimSpace(t.ID)
		if t.ID == "" {
			continue
		}
		if _, exists := byID[t.ID]; exists {
			continue
		}
		byID[t.ID] = t
		order = append(order, t.ID)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := byID[order[i]], byID[order[j]]
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Title < b.Title
	})
	return &Store{path: path, byID: byID, order: order}
}

// List returns every prompt in deterministic template order.
func (s *Store) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.entryLocked(id))
	}
	return out, nil
}

// Get returns the effective prompt for id and whether it exists.
func (s *Store) Get(id string) (Entry, bool, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return Entry{}, false, err
	}
	if _, ok := s.byID[id]; !ok {
		return Entry{}, false, nil
	}
	return s.entryLocked(id), true, nil
}

// Text returns the effective text for id, or an empty string on miss or error.
func (s *Store) Text(id string) string {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		if t, ok := s.byID[id]; ok {
			return strings.TrimSpace(t.DefaultText)
		}
		return ""
	}
	return s.entryLocked(id).Text
}

// OverrideText returns only the stored override for id.
func (s *Store) OverrideText(id string) (string, bool) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return "", false
	}
	ov, ok := s.overrides[id]
	if !ok {
		return "", false
	}
	return strings.TrimSpace(ov.Text), true
}

// Set persists an override and returns the resulting effective entry.
func (s *Store) Set(id, text string) (Entry, error) {
	id = strings.TrimSpace(id)
	text = strings.TrimSpace(text)
	if text == "" {
		return Entry{}, ErrEmpty
	}
	if len([]rune(text)) > maxPromptRunes {
		return Entry{}, ErrTooLarge
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return Entry{}, err
	}
	t, ok := s.byID[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	if !t.Editable {
		return Entry{}, ErrReadOnly
	}
	previous, hadPrevious := s.overrides[id]
	s.overrides[id] = overrideEntry{Text: text, UpdatedAtMs: time.Now().UnixMilli()}
	if err := s.saveLocked(); err != nil {
		if hadPrevious {
			s.overrides[id] = previous
		} else {
			delete(s.overrides, id)
		}
		return Entry{}, err
	}
	return s.entryLocked(id), nil
}

// Reset removes an override and restores the built-in prompt text.
func (s *Store) Reset(id string) (Entry, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return Entry{}, err
	}
	if _, ok := s.byID[id]; !ok {
		return Entry{}, ErrNotFound
	}
	previous, hadPrevious := s.overrides[id]
	delete(s.overrides, id)
	if err := s.saveLocked(); err != nil {
		if hadPrevious {
			s.overrides[id] = previous
		}
		return Entry{}, err
	}
	return s.entryLocked(id), nil
}

func (s *Store) entryLocked(id string) Entry {
	t := s.byID[id]
	ov, overridden := s.overrides[id]
	text := strings.TrimSpace(t.DefaultText)
	updated := int64(0)
	if overridden {
		text = strings.TrimSpace(ov.Text)
		updated = ov.UpdatedAtMs
	}
	return Entry{
		ID:          id,
		Title:       t.Title,
		Description: t.Description,
		Category:    t.Category,
		Text:        text,
		DefaultText: strings.TrimSpace(t.DefaultText),
		Editable:    t.Editable,
		Overridden:  overridden,
		UpdatedAtMs: updated,
	}
}

func (s *Store) loadLocked() error {
	if s.loaded {
		return nil
	}
	s.overrides = map[string]overrideEntry{}
	if s.path == "" {
		s.loaded = true
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.loaded = true
			return nil
		}
		return fmt.Errorf("prompt overrides read: %w", err)
	}
	var f overrideFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("prompt overrides parse: %w", err)
	}
	for id, ov := range f.Prompts {
		id = strings.TrimSpace(id)
		if _, ok := s.byID[id]; !ok {
			continue
		}
		if text := strings.TrimSpace(ov.Text); text != "" {
			ov.Text = text
			s.overrides[id] = ov
		}
	}
	s.loaded = true
	return nil
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	f := overrideFile{Prompts: s.overrides}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("prompt overrides marshal: %w", err)
	}
	data = append(data, '\n')
	return atomicfile.WriteFile(s.path, data, &atomicfile.Options{Perm: 0o600, DirPerm: 0o700, Fsync: true, Backup: true})
}
