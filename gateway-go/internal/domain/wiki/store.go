package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// ValidateCategory returns true if cat is one of the allowed wiki categories.
func ValidateCategory(cat string) bool {
	for _, c := range Categories {
		if c == cat {
			return true
		}
	}
	return false
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

	// Load or create master index, then prune ghost entries.
	idx, err := s.loadOrCreateIndex()
	if err != nil {
		return nil, fmt.Errorf("wiki: load index: %w", err)
	}
	s.index = idx
	s.pruneGhostEntries()

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
	_, readErr := s.ReadPage(relPath)
	op := "update"
	if readErr != nil {
		op = "create"
	}
	if err := s.writePageInternal(relPath, page, false); err != nil {
		return err
	}
	_ = s.AppendLog(op, relPath+" — "+page.Meta.Title) // best-effort: audit log is non-critical
	return nil
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
		_ = s.fts.indexPage(relPath, page) // best-effort: FTS index is non-critical
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
		_ = s.fts.removePage(relPath) // best-effort: FTS cleanup is non-critical
	}

	s.mu.Lock()
	s.index.RemoveEntry(relPath)
	if err := s.index.Save(filepath.Join(s.dir, "index.md")); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	_ = s.AppendLog("delete", relPath) // best-effort: audit log is non-critical

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
		if _, ok := oldSet[target]; ok {
			continue // already linked
		}
		s.addBacklink(target, relPath)
	}

	// Remove relPath from no-longer-related pages.
	for _, target := range oldRelated {
		if _, ok := newSet[target]; ok {
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
	_ = s.writePageInternal(targetPath, page, true) // best-effort: related page update is non-critical
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
	_ = s.writePageInternal(targetPath, page, true) // best-effort: related page update is non-critical
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
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
			return nil //nolint:nilerr // skip inaccessible entries in walk
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		// Skip index and log files.
		base := filepath.Base(path)
		if base == "index.md" || base == "_index.md" || base == "log.md" {
			return nil
		}
		rel, _ := filepath.Rel(s.dir, path)
		pages = append(pages, rel)
		return nil
	})
	return pages, err
}

// Index returns the cached master index.
func (s *Store) Index() *Index {
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

// AppendLog appends a timestamped operation entry to log.md in the wiki root.
// Tracks all wiki mutations for temporal awareness (Karpathy wiki concept).
func (s *Store) AppendLog(operation, details string) error {
	logPath := filepath.Join(s.dir, "log.md")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("wiki: open log: %w", err)
	}
	defer f.Close()
	ts := time.Now().Format("2006-01-02 15:04")
	entry := fmt.Sprintf("## [%s] %s\n%s\n\n", ts, operation, details)
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
			err = idx.Save(indexPath)
			return idx, err
		}
		return nil, err
	}
	return idx, nil
}

// SplitPage splits an oversized page into sub-pages by H2 sections.
// The parent page is rewritten as a table-of-contents linking to sub-pages.
// Sub-pages inherit the parent's metadata.
// Returns the paths of created sub-pages, or nil if splitting was not needed.
func (s *Store) SplitPage(relPath string, maxBytes int) ([]string, error) {
	page, err := s.ReadPage(relPath)
	if err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}

	// Check actual size.
	abs := filepath.Join(s.dir, relPath)
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat page: %w", err)
	}
	if info.Size() <= int64(maxBytes) {
		return nil, nil
	}

	preamble, sections := page.SplitByH2()
	if len(sections) < 2 {
		// Cannot split meaningfully — single section or no H2 headings.
		return nil, nil
	}

	// Build sub-pages by grouping consecutive sections under the byte budget.
	parentDir := filepath.Dir(relPath)
	parentBase := strings.TrimSuffix(filepath.Base(relPath), ".md")

	type subPage struct {
		path     string
		sections []H2Section
	}

	var subs []subPage
	var currentSections []H2Section
	var currentSize int

	// Reserve budget for frontmatter overhead (~300 bytes).
	budget := maxBytes - 300

	for _, sec := range sections {
		secSize := len(sec.Heading) + len(sec.Content) + 10 // "## heading\n\ncontent\n"
		if len(currentSections) > 0 && currentSize+secSize > budget {
			slug := sectionSlug(currentSections[0].Heading)
			subPath := filepath.Join(parentDir, parentBase+"--"+slug+".md")
			subs = append(subs, subPage{path: subPath, sections: currentSections})
			currentSections = nil
			currentSize = 0
		}
		currentSections = append(currentSections, sec)
		currentSize += secSize
	}
	if len(currentSections) > 0 {
		slug := sectionSlug(currentSections[0].Heading)
		subPath := filepath.Join(parentDir, parentBase+"--"+slug+".md")
		subs = append(subs, subPage{path: subPath, sections: currentSections})
	}

	// If everything fits in one sub-page, splitting is pointless.
	if len(subs) <= 1 {
		return nil, nil
	}

	// Create sub-pages.
	var createdPaths []string
	var tocLines []string
	today := time.Now().Format("2006-01-02")

	for _, sp := range subs {
		sub := NewPage(page.Meta.Title+" — "+sp.sections[0].Heading, page.Meta.Category, page.Meta.Tags)
		sub.Meta.Importance = page.Meta.Importance
		sub.Meta.Related = []string{relPath}

		var body strings.Builder
		for _, sec := range sp.sections {
			body.WriteString("## " + sec.Heading + "\n\n")
			if sec.Content != "" {
				body.WriteString(sec.Content + "\n\n")
			}
		}
		sub.Body = strings.TrimSpace(body.String())

		if err := s.WritePage(sp.path, sub); err != nil {
			return createdPaths, fmt.Errorf("write sub-page %s: %w", sp.path, err)
		}
		createdPaths = append(createdPaths, sp.path)

		label := sp.sections[0].Heading
		tocLines = append(tocLines, fmt.Sprintf("- [[%s]] — %s", sp.path, label))
	}

	// Rewrite parent page as a TOC.
	page.Meta.Updated = today
	var parentBody strings.Builder
	if preamble != "" {
		parentBody.WriteString(preamble + "\n\n")
	}
	parentBody.WriteString("## 하위 문서\n\n")
	parentBody.WriteString(strings.Join(tocLines, "\n"))
	parentBody.WriteByte('\n')
	page.Body = parentBody.String()
	page.Meta.Related = append(page.Meta.Related, createdPaths...)

	if err := s.WritePage(relPath, page); err != nil {
		return createdPaths, fmt.Errorf("rewrite parent: %w", err)
	}

	return createdPaths, nil
}

// sectionSlug converts a section heading to a short file-safe slug.
func sectionSlug(heading string) string {
	heading = strings.ToLower(heading)
	var sb strings.Builder
	for _, r := range heading {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r >= 0xAC00 && r <= 0xD7A3: // Hangul syllable
			sb.WriteRune(r)
		case r >= 0x3131 && r <= 0x318E: // Hangul jamo
			sb.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			sb.WriteByte('-')
		}
	}
	slug := strings.TrimRight(sb.String(), "-")
	if len([]rune(slug)) > 30 {
		slug = string([]rune(slug)[:30])
	}
	if slug == "" {
		slug = "section"
	}
	return slug
}

// pruneGhostEntries removes index entries whose files no longer exist on disk.
func (s *Store) pruneGhostEntries() {
	var ghosts []string
	for relPath := range s.index.Entries {
		abs := filepath.Join(s.dir, relPath)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			ghosts = append(ghosts, relPath)
		}
	}
	if len(ghosts) == 0 {
		return
	}
	for _, g := range ghosts {
		delete(s.index.Entries, g)
	}
	_ = s.index.Save(filepath.Join(s.dir, "index.md")) // best-effort: index save is non-critical
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
