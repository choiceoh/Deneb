package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Categories are the top-level wiki directories.
var Categories = []string{
	"사람",
	"프로젝트",
	"기술",
	"업무",
	"결정",
	"선호",
}

// Store manages a wiki directory on disk.
type Store struct {
	dir      string
	diaryDir string

	mu    sync.RWMutex
	index *Index // cached master index
	fts   *searchDB
}

// NewStore creates a wiki store rooted at dir.
// It ensures the directory structure exists.
func NewStore(dir, diaryDir string) (*Store, error) {
	if err := ensureDirs(dir); err != nil {
		return nil, fmt.Errorf("wiki: ensure dirs: %w", err)
	}
	s := &Store{dir: dir, diaryDir: diaryDir}

	// Load or create master index.
	idx, err := s.loadOrCreateIndex()
	if err != nil {
		return nil, fmt.Errorf("wiki: load index: %w", err)
	}
	s.index = idx

	// Initialize FTS search index.
	fts, err := newSearchDB(dir)
	if err != nil {
		return nil, fmt.Errorf("wiki: init search: %w", err)
	}
	s.fts = fts

	// Rebuild FTS from disk on startup.
	if err := fts.rebuildIndex(dir); err != nil {
		fts.close()
		return nil, fmt.Errorf("wiki: rebuild search index: %w", err)
	}

	return s, nil
}

// Dir returns the wiki root directory.
func (s *Store) Dir() string { return s.dir }

// DiaryDir returns the diary directory for raw daily logs.
func (s *Store) DiaryDir() string { return s.diaryDir }

// ReadPage reads a wiki page by relative path (e.g., "기술/dgx-spark.md").
func (s *Store) ReadPage(relPath string) (*Page, error) {
	abs := filepath.Join(s.dir, relPath)
	return ParsePageFile(abs)
}

// WritePage writes a page to the wiki. Creates parent directories if needed.
// Updates the master index entry and maintains bidirectional backlinks.
func (s *Store) WritePage(relPath string, page *Page) error {
	return s.writePageInternal(relPath, page, false)
}

func (s *Store) writePageInternal(relPath string, page *Page, skipBacklinks bool) error {
	abs := filepath.Join(s.dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("wiki: mkdir: %w", err)
	}
	if err := WritePageFile(abs, page); err != nil {
		return err
	}

	// Update FTS index.
	if s.fts != nil {
		_ = s.fts.indexPage(relPath, page)
	}

	// Capture old related list before updating index.
	s.mu.Lock()
	var oldRelated []string
	if old, ok := s.index.Entries[relPath]; ok {
		oldRelated = old.Related
	}
	s.index.UpdateEntry(relPath, page)
	if err := s.index.Save(filepath.Join(s.dir, "index.md")); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	// Maintain bidirectional backlinks.
	if !skipBacklinks {
		s.maintainBacklinks(relPath, oldRelated, page.Meta.Related)
	}
	return nil
}

// DeletePage removes a page and its index entry.
// Cleans up backlinks from related pages.
func (s *Store) DeletePage(relPath string) error {
	// Read page before deleting to get its related list.
	var oldRelated []string
	if page, err := s.ReadPage(relPath); err == nil {
		oldRelated = page.Meta.Related
	}

	abs := filepath.Join(s.dir, relPath)
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wiki: delete: %w", err)
	}

	// Update FTS index.
	if s.fts != nil {
		_ = s.fts.removePage(relPath)
	}

	s.mu.Lock()
	s.index.RemoveEntry(relPath)
	if err := s.index.Save(filepath.Join(s.dir, "index.md")); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	// Remove backlinks: remove relPath from each formerly-related page.
	s.maintainBacklinks(relPath, oldRelated, nil)
	return nil
}

// maintainBacklinks ensures bidirectional Related links.
// It compares oldRelated (previous state) with newRelated (current state)
// and updates target pages accordingly.
func (s *Store) maintainBacklinks(relPath string, oldRelated, newRelated []string) {
	oldSet := toSet(oldRelated)
	newSet := toSet(newRelated)

	// Add relPath to newly-related pages.
	for _, target := range newRelated {
		if oldSet[target] {
			continue // already linked
		}
		s.addBacklink(target, relPath)
	}

	// Remove relPath from no-longer-related pages.
	for _, target := range oldRelated {
		if newSet[target] {
			continue // still linked
		}
		s.removeBacklink(target, relPath)
	}
}

func (s *Store) addBacklink(targetPath, sourcePath string) {
	page, err := s.ReadPage(targetPath)
	if err != nil {
		return // target doesn't exist — skip
	}
	for _, r := range page.Meta.Related {
		if r == sourcePath {
			return // already present
		}
	}
	page.Meta.Related = append(page.Meta.Related, sourcePath)
	page.Meta.Updated = time.Now().Format("2006-01-02")
	_ = s.writePageInternal(targetPath, page, true)
}

func (s *Store) removeBacklink(targetPath, sourcePath string) {
	page, err := s.ReadPage(targetPath)
	if err != nil {
		return
	}
	filtered := page.Meta.Related[:0]
	for _, r := range page.Meta.Related {
		if r != sourcePath {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == len(page.Meta.Related) {
		return // nothing changed
	}
	page.Meta.Related = filtered
	page.Meta.Updated = time.Now().Format("2006-01-02")
	_ = s.writePageInternal(targetPath, page, true)
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// ListPages returns all page paths in a category (e.g., "기술").
// If category is empty, returns all pages.
func (s *Store) ListPages(category string) ([]string, error) {
	var searchDir string
	if category != "" {
		searchDir = filepath.Join(s.dir, category)
	} else {
		searchDir = s.dir
	}

	var pages []string
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		// Skip index files.
		base := filepath.Base(path)
		if base == "index.md" || base == "_index.md" {
			return nil
		}
		rel, _ := filepath.Rel(s.dir, path)
		pages = append(pages, rel)
		return nil
	})
	return pages, err
}

// GetIndex returns the cached master index.
func (s *Store) GetIndex() *Index {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.index
}

// Tier1Pages returns all non-archived pages with importance >= minImportance,
// sorted by importance descending. Each result includes the page path and content.
func (s *Store) Tier1Pages(minImportance float64) []Tier1Result {
	s.mu.RLock()
	idx := s.index
	s.mu.RUnlock()

	var results []Tier1Result
	for path, entry := range idx.Entries {
		if entry.Importance < minImportance {
			continue
		}
		page, err := s.ReadPage(path)
		if err != nil || page.Meta.Archived {
			continue
		}
		results = append(results, Tier1Result{
			Path: path,
			Page: page,
		})
	}

	// Sort by importance descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Page.Meta.Importance > results[j].Page.Meta.Importance
	})
	return results
}

// Tier1Result is a high-importance wiki page for auto-injection.
type Tier1Result struct {
	Path string
	Page *Page
}

// Stats returns wiki statistics.
func (s *Store) Stats() StoreStats {
	pages, _ := s.ListPages("")
	var totalBytes int64
	catCount := map[string]int{}
	for _, p := range pages {
		abs := filepath.Join(s.dir, p)
		if info, err := os.Stat(abs); err == nil {
			totalBytes += info.Size()
		}
		cat := filepath.Dir(p)
		if cat == "." {
			cat = "(root)"
		}
		catCount[cat]++
	}

	return StoreStats{
		TotalPages:    len(pages),
		TotalBytes:    totalBytes,
		CategoryCount: catCount,
	}
}

// StoreStats holds wiki statistics.
type StoreStats struct {
	TotalPages    int
	TotalBytes    int64
	CategoryCount map[string]int
}

// AppendDiary appends a timestamped entry to today's diary file.
// Safe to call from any goroutine. Creates the diary directory and file if needed.
func (s *Store) AppendDiary(content string) error {
	return AppendDiaryTo(s.diaryDir, content)
}

// AppendDiaryTo appends a timestamped entry to today's diary file in the given directory.
// Standalone function usable without a Store instance.
func AppendDiaryTo(diaryDir, content string) error {
	if content == "" || diaryDir == "" {
		return nil
	}
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		return fmt.Errorf("diary mkdir: %w", err)
	}
	now := time.Now()
	path := filepath.Join(diaryDir, "diary-"+now.Format("2006-01-02")+".md")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("diary open: %w", err)
	}
	defer f.Close()

	entry := fmt.Sprintf("\n## %s\n\n%s\n", now.Format("15:04"), content)
	_, err = f.WriteString(entry)
	return err
}

// Close releases the FTS search database.
func (s *Store) Close() error {
	if s.fts != nil {
		return s.fts.close()
	}
	return nil
}

func (s *Store) loadOrCreateIndex() (*Index, error) {
	indexPath := filepath.Join(s.dir, "index.md")
	idx, err := ParseIndex(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			idx = NewIndex()
			return idx, idx.Save(indexPath)
		}
		return nil, err
	}
	return idx, nil
}

func ensureDirs(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, cat := range Categories {
		if err := os.MkdirAll(filepath.Join(dir, cat), 0o755); err != nil {
			return err
		}
	}
	return nil
}
