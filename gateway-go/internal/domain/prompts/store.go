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

type Template struct {
	ID          string
	Title       string
	Description string
	Category    string
	DefaultText string
	Editable    bool
}

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

type Store struct {
	path      string
	byID      map[string]Template
	order     []string
	mu        sync.Mutex
	overrides map[string]overrideEntry
	loaded    bool
}

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
	s.overrides[id] = overrideEntry{Text: text, UpdatedAtMs: time.Now().UnixMilli()}
	if err := s.saveLocked(); err != nil {
		return Entry{}, err
	}
	return s.entryLocked(id), nil
}

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
	delete(s.overrides, id)
	if err := s.saveLocked(); err != nil {
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
