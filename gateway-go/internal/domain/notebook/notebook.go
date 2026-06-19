// Package notebook implements NotebookLM-style scoped source collections.
//
// A Notebook is a user-curated set of "sources" (pinned wiki pages or pasted
// notes) that the agent can synthesize a *grounded, cited* briefing over — the
// answer draws ONLY on the pinned sources and cites each claim back to a stable
// per-notebook tag (S1, S2, ...). Where Deneb's recall preflight searches the
// whole memory corpus implicitly, a notebook is the explicit, narrow scope:
// "reason over JUST these items for this deal."
//
// Phase 1 supports two source kinds — wiki (read live at brief time, so the
// briefing always reflects the current page) and note (inline pasted text,
// self-contained). Mail threads / URLs / diary entries are deferred to a later
// phase when their read dependencies are wired through.
//
// Single-user, single-machine: the store is a plain directory of one JSON file
// per notebook, loaded into memory at startup and re-saved on every mutation.
package notebook

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
	"unicode"
)

// Source kinds supported in Phase 1.
const (
	KindWiki = "wiki" // Ref = wiki page path (e.g. "프로젝트/topsolar.md"); content read live at brief time.
	KindNote = "note" // Text = pasted inline content (email body, quote, meeting note); self-contained.
)

// ErrNotFound is returned when a notebook id does not exist.
var ErrNotFound = errors.New("notebook: not found")

// Source is one pinned item in a notebook. Cite is a stable per-notebook
// citation tag ("S1", "S2", ...) the briefing model references inline so the
// reader can trace each claim back to its origin. Cites are never reused: a
// removal leaves a gap rather than renumbering, so an existing [S3] always
// means the same source even as the collection changes.
type Source struct {
	Cite  string `json:"cite"`
	Kind  string `json:"kind"`
	Ref   string `json:"ref,omitempty"`   // wiki page path (KindWiki)
	Title string `json:"title,omitempty"` // human label
	Text  string `json:"text,omitempty"`  // inline content (KindNote)
	Added int64  `json:"added"`           // unix millis
}

// Notebook is a user-scoped collection of sources for grounded synthesis.
type Notebook struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Sources     []Source `json:"sources"`
	Created     int64    `json:"created"` // unix millis
	Updated     int64    `json:"updated"` // unix millis
}

// Store is a directory-backed collection of notebooks, guarded by a single
// mutex (single-user traffic is serial, so coarse locking is fine).
type Store struct {
	dir string
	mu  sync.Mutex
	nbs map[string]*Notebook
}

// NewStore opens (creating if needed) a notebook store rooted at dir and loads
// any existing notebooks from disk.
func NewStore(dir string) (*Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("notebook: empty dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("notebook: mkdir %s: %w", dir, err)
	}
	s := &Store{dir: dir, nbs: make(map[string]*Notebook)}
	s.loadAll()
	return s, nil
}

// loadAll reads every *.json under the store dir. Unreadable/corrupt files are
// skipped (best-effort) rather than failing startup.
func (s *Store) loadAll() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var nb Notebook
		if json.Unmarshal(data, &nb) != nil || nb.ID == "" {
			continue
		}
		s.nbs[nb.ID] = &nb
	}
}

// Create makes a new empty notebook. The id is a slug of the name, made unique
// against existing notebooks.
func (s *Store) Create(name, description string) (*Notebook, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("notebook: name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.uniqueIDLocked(slugify(name))
	now := time.Now().UnixMilli()
	nb := &Notebook{ID: id, Name: name, Description: strings.TrimSpace(description), Created: now, Updated: now}
	s.nbs[id] = nb
	if err := s.saveLocked(nb); err != nil {
		delete(s.nbs, id)
		return nil, err
	}
	return clone(nb), nil
}

// Get returns a copy of the notebook with the given id.
func (s *Store) Get(id string) (*Notebook, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nb, ok := s.nbs[id]
	if !ok {
		return nil, false
	}
	return clone(nb), true
}

// List returns copies of all notebooks, most-recently-updated first.
func (s *Store) List() []*Notebook {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Notebook, 0, len(s.nbs))
	for _, nb := range s.nbs {
		out = append(out, clone(nb))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out
}

// Delete removes a notebook and its on-disk file.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.nbs[id]; !ok {
		return ErrNotFound
	}
	delete(s.nbs, id)
	// Best-effort file removal: a missing file is not an error worth surfacing.
	_ = os.Remove(filepath.Join(s.dir, id+".json"))
	return nil
}

// AddSource pins a source to a notebook, assigning it the next stable cite tag.
// The caller fills Kind plus Ref/Text/Title; Cite and Added are set here.
func (s *Store) AddSource(id string, src Source) (*Source, error) {
	if err := validateSource(src); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	nb, ok := s.nbs[id]
	if !ok {
		return nil, ErrNotFound
	}
	src.Cite = nextCite(nb.Sources)
	src.Added = time.Now().UnixMilli()
	src.Ref = strings.TrimSpace(src.Ref)
	src.Title = strings.TrimSpace(src.Title)
	nb.Sources = append(nb.Sources, src)
	nb.Updated = src.Added
	if err := s.saveLocked(nb); err != nil {
		// Roll back the in-memory append so memory and disk stay consistent.
		nb.Sources = nb.Sources[:len(nb.Sources)-1]
		return nil, err
	}
	added := src
	return &added, nil
}

// RemoveSource unpins the source with the given cite tag. The remaining cites
// are left untouched (gaps are fine — cites are stable, not contiguous).
func (s *Store) RemoveSource(id, cite string) error {
	cite = strings.TrimSpace(cite)
	s.mu.Lock()
	defer s.mu.Unlock()
	nb, ok := s.nbs[id]
	if !ok {
		return ErrNotFound
	}
	idx := -1
	for i, src := range nb.Sources {
		if strings.EqualFold(src.Cite, cite) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("notebook: source %q not found", cite)
	}
	removed := nb.Sources[idx]
	nb.Sources = append(nb.Sources[:idx], nb.Sources[idx+1:]...)
	nb.Updated = time.Now().UnixMilli()
	if err := s.saveLocked(nb); err != nil {
		// Restore on save failure to keep memory consistent with disk.
		nb.Sources = append(nb.Sources, Source{})
		copy(nb.Sources[idx+1:], nb.Sources[idx:])
		nb.Sources[idx] = removed
		return err
	}
	return nil
}

// validateSource checks kind-specific required fields before mutation.
func validateSource(src Source) error {
	switch src.Kind {
	case KindWiki:
		if strings.TrimSpace(src.Ref) == "" {
			return errors.New("notebook: wiki source requires ref (page path)")
		}
	case KindNote:
		if strings.TrimSpace(src.Text) == "" {
			return errors.New("notebook: note source requires text")
		}
	default:
		return fmt.Errorf("notebook: unsupported source kind %q (supported: wiki, note)", src.Kind)
	}
	return nil
}

// nextCite returns one past the highest existing Sn so cites never collide,
// even after removals have left gaps.
func nextCite(sources []Source) string {
	max := 0
	for _, src := range sources {
		if n, ok := parseCite(src.Cite); ok && n > max {
			max = n
		}
	}
	return "S" + strconv.Itoa(max+1)
}

func parseCite(cite string) (int, bool) {
	if !strings.HasPrefix(cite, "S") {
		return 0, false
	}
	n, err := strconv.Atoi(cite[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// uniqueIDLocked returns base, or base-2/base-3/... if base is taken.
func (s *Store) uniqueIDLocked(base string) string {
	if _, ok := s.nbs[base]; !ok {
		return base
	}
	for i := 2; ; i++ {
		cand := base + "-" + strconv.Itoa(i)
		if _, ok := s.nbs[cand]; !ok {
			return cand
		}
	}
}

// saveLocked atomically writes a notebook to <dir>/<id>.json. Caller holds mu.
func (s *Store) saveLocked(nb *Notebook) error {
	data, err := json.MarshalIndent(nb, "", "  ")
	if err != nil {
		return fmt.Errorf("notebook: marshal %s: %w", nb.ID, err)
	}
	final := filepath.Join(s.dir, nb.ID+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("notebook: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("notebook: rename %s: %w", final, err)
	}
	return nil
}

// clone returns a deep copy so callers cannot mutate the store's state.
func clone(nb *Notebook) *Notebook {
	cp := *nb
	if len(nb.Sources) > 0 {
		cp.Sources = append([]Source(nil), nb.Sources...)
	}
	return &cp
}

// slugify turns a notebook name into a filesystem- and ref-friendly id,
// keeping unicode letters/digits (so Korean names slug sensibly) and folding
// whitespace/separators to '-'.
func slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == ' ' || r == '\t' || r == '/' || r == '\\' || r == '_':
			b.WriteRune('-')
		}
	}
	s := strings.Trim(collapseDashes(b.String()), "-")
	if r := []rune(s); len(r) > 40 {
		s = strings.Trim(string(r[:40]), "-")
	}
	if s == "" {
		s = "notebook"
	}
	return s
}

func collapseDashes(s string) string {
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}
