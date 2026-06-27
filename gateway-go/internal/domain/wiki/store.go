package wiki

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// Categories are the top-level wiki directories — the fixed 6-category taxonomy.
//
//	프로젝트 — 진행 중인 일·거래·결정 (거래/결정/메일분석은 하위 폴더로 흡수)
//	인물     — 사람·조직 (연락처·관계·담당자)
//	시스템   — Deneb 자신의 구성·운영 (서버·모델·배포·도구 설정)
//	업무     — 직무 도메인 지식 (태양광·전선·구리값 등 일에 직접 쓰이는 지식)
//	사용자   — 사용자 개인 (선호·톤 규칙·개인 컨텍스트)
//	기타     — 그 외 일반/세상 지식 (국제정세·시사·잡학) + catch-all
//
// The bucket a page lands in is its path's leading directory (Stats uses
// filepath.Dir), not its frontmatter category field — so the write paths
// (dreamer, deals, mail-analysis) keep page paths under these directories.
var Categories = []string{
	"프로젝트",
	"인물",
	"시스템",
	"업무",
	"사용자",
	"기타",
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
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	Store.writeMu  →  Store.mu
//	searchDB.mu (s.fts / s.diaryFTS) is an independent leaf, taken on its own.
//
// writeMu serializes the read-modify-write of a page body (read file → mutate →
// atomic temp+rename) together with its index update, so two writers on the same
// page can't interleave (read,read,write,write) and clobber each other's edit —
// the last-writer-wins lost update this guards against. It is acquired exactly
// once at each public write boundary (WritePage, UpdatePage, DeletePage,
// MarkSuperseded, MergePage, SplitPage, UpsertDealPage, EnrichPeople's person-page
// helpers, RebuildIndex). The internal *Locked helpers and writePageInternal / maintainBacklinks
// assume it is already held and never re-acquire it (Go mutexes are non-reentrant).
// mu independently guards the in-memory index/backlink maps so pure readers
// (Index, Tier1Pages, Search) never block behind a write's disk I/O.
type Store struct {
	dir      string
	diaryDir string

	// writeMu serializes page-body writers; see the type doc for the hierarchy.
	writeMu sync.Mutex

	// dealMu serializes appends/reads of the typed deal-record ledger
	// (.deals.jsonl), independent of page writes. See deal_records.go.
	dealMu sync.Mutex

	mu       sync.RWMutex
	index    *Index // cached master index
	fts      *searchDB
	diaryFTS *diarySearchDB

	// sem is the optional semantic (embedding) index. nil until SetEmbedder is
	// called; when present, Search blends BM25 with dense-vector neighbors so a
	// query finds pages by meaning, not just keyword overlap. Degrades silently
	// to pure BM25 whenever the embedding server is unavailable.
	sem *semanticIndex
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

	// Initialize in-memory search index (rebuilt from .md files on startup).
	fts := newSearchDB()
	s.fts = fts
	if err := fts.rebuildIndex(dir); err != nil {
		return nil, fmt.Errorf("wiki: rebuild search index: %w", err)
	}

	// Initialize in-memory diary search index from the diary directory.
	// Missing or empty diary dir is fine — search will simply return zero hits.
	diaryFTS := newDiarySearchDB()
	s.diaryFTS = diaryFTS
	if err := diaryFTS.rebuildFromDir(diaryDir); err != nil {
		return nil, fmt.Errorf("wiki: rebuild diary index: %w", err)
	}

	return s, nil
}

// Dir returns the wiki root directory.
func (s *Store) Dir() string { return s.dir }

// DiaryDir returns the diary directory for raw daily logs.
func (s *Store) DiaryDir() string { return s.diaryDir }

// normalizePagePath ensures a wiki page path carries the .md extension.
//
// Wiki pages are always stored as .md files, but callers pass paths from many
// sources — RPC clients, the dreamer's LLM-proposed paths, the wiki tool — and
// some omit the extension. Centralizing the fix-up here means "프로젝트/foo" and
// "프로젝트/foo.md" resolve to the same file. Without it, a bare path writes an
// extensionless sibling that ListPages (which filters on .md) silently drops
// from search and the master index, which in turn defeats duplicate detection
// and lets the same page be created over and over.
func normalizePagePath(relPath string) string {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return relPath
	}
	if !strings.HasSuffix(relPath, ".md") {
		relPath += ".md"
	}
	return relPath
}

// ReadPage reads a wiki page by relative path (e.g., "기술/dgx-spark.md").
// The .md extension is optional; it is appended when absent.
func (s *Store) ReadPage(relPath string) (*Page, error) {
	relPath = normalizePagePath(relPath)
	abs := filepath.Join(s.dir, relPath)
	return ParsePageFile(abs)
}

// WritePage writes a page to the wiki. Creates parent directories if needed.
// Updates the master index entry and maintains bidirectional backlinks.
//
// Holds writeMu for the whole write so it can't interleave with a concurrent
// UpdatePage/WritePage on the same path. WritePage takes a fully-formed page and
// overwrites the body wholesale — for a read-modify-write (append to an existing
// body, merge fields) use UpdatePage so the read and write are one atomic step;
// a bare ReadPage→WritePage pair from a caller is still racy because the read
// happens outside this lock.
func (s *Store) WritePage(relPath string, page *Page) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writePageLocked(relPath, page)
}

// writePageLocked is WritePage's body; the caller must hold writeMu.
func (s *Store) writePageLocked(relPath string, page *Page) error {
	relPath = normalizePagePath(relPath)
	// Defend every write path (dreamer, wiki tool, RPC, miniapp merge) against
	// content that arrives with its own frontmatter prepended — storing it as a
	// body would stack a duplicate on-disk frontmatter. See StripLeadingFrontmatter.
	if page != nil {
		page.Body = StripLeadingFrontmatter(page.Body)
	}
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

// UpdatePage atomically reads the page at relPath, hands it to mutate, and writes
// back whatever mutate returns — all under writeMu, so a concurrent
// UpdatePage/WritePage on the same path can't interleave and lose an update.
//
// This is the read-modify-write primitive every appending/merging writer should
// use (deal upserts, the dreamer's update branch, the wiki tool, contact
// enrichment): doing the read and the write as two separate Store calls leaves a
// window where another writer's rename clobbers the just-read content.
//
// mutate receives the current page, or nil when the page does not exist yet
// (mirroring the existing "read error ⇒ treat as create" behavior), and returns
// the page to persist. Returning a nil page with a nil error skips the write —
// use it for a no-op update (idempotent re-file, unchanged section) so the Updated
// date and the index don't churn. Backlinks, the index, and the audit log are
// maintained exactly as WritePage does.
func (s *Store) UpdatePage(relPath string, mutate func(current *Page) (*Page, error)) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	current, _ := s.ReadPage(relPath) // nil when absent/unreadable — same as the create path
	next, err := mutate(current)
	if err != nil || next == nil {
		return err
	}
	return s.writePageLocked(relPath, next)
}

// writePageInternal writes the page file, updates the search + master index, and
// (unless skipBacklinks) maintains bidirectional backlinks. The caller must hold
// writeMu — every path that reaches here (writePageLocked, deletePageLocked,
// MarkSuperseded, MergePage, repointReference, backlink maintenance) holds it.
func (s *Store) writePageInternal(relPath string, page *Page, skipBacklinks bool) error {
	relPath = normalizePagePath(relPath)
	abs := filepath.Join(s.dir, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("wiki: mkdir: %w", err)
	}
	if err := WritePageFile(abs, page); err != nil {
		return err
	}

	// Update search index.
	if s.fts != nil {
		s.fts.indexPage(relPath, page)
	}

	// Capture old related list before updating index.
	var oldRelated []string
	if err := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if old, ok := s.index.Entries[relPath]; ok {
			oldRelated = old.Related
		}
		s.index.UpdateEntry(relPath, page)
		return s.index.Save(filepath.Join(s.dir, "index.md"))
	}(); err != nil {
		return err
	}

	// Maintain bidirectional backlinks.
	if !skipBacklinks {
		s.maintainBacklinks(relPath, oldRelated, page.Meta.Related)
	}
	return nil
}

// DeletePage removes a page and its index entry.
// Cleans up backlinks from related pages. Serialized against page writes via
// writeMu so a delete can't interleave with a concurrent write of the same page.
func (s *Store) DeletePage(relPath string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.deletePageLocked(relPath)
}

// deletePageLocked is DeletePage's body; the caller must hold writeMu (MergePage
// deletes the merge source while already holding it).
func (s *Store) deletePageLocked(relPath string) error {
	relPath = normalizePagePath(relPath)
	// Read page before deleting to get its related list.
	var oldRelated []string
	if page, err := s.ReadPage(relPath); err == nil {
		oldRelated = page.Meta.Related
	}

	abs := filepath.Join(s.dir, relPath)
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wiki: delete: %w", err)
	}

	// Update search index.
	if s.fts != nil {
		s.fts.removePage(relPath)
	}

	if err := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.index.RemoveEntry(relPath)
		return s.index.Save(filepath.Join(s.dir, "index.md"))
	}(); err != nil {
		return err
	}

	_ = s.AppendLog("delete", relPath) // best-effort: audit log is non-critical

	// Remove backlinks: remove relPath from each formerly-related page.
	s.maintainBacklinks(relPath, oldRelated, nil)
	return nil
}

// maintainBacklinks ensures bidirectional Related links.
// It compares oldRelated (previous state) with newRelated (current state)
// and updates target pages accordingly. The caller holds writeMu, so the target
// pages it rewrites (addBacklink/removeBacklink → writePageInternal) are part of
// the same serialized write — a single global lock means no cross-page ordering
// deadlock between two writers maintaining each other's backlinks.
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
	// Best-effort: a failed reverse edge is non-fatal, but silent failures let
	// the graph drift apart over months — surface them for the operator.
	if err := s.writePageInternal(targetPath, page, true); err != nil {
		slog.Warn("wiki: backlink add failed; graph edge now one-directional",
			"target", targetPath, "source", sourcePath, "error", err)
	}
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
	if err := s.writePageInternal(targetPath, page, true); err != nil {
		slog.Warn("wiki: backlink removal failed; stale reverse edge remains",
			"target", targetPath, "source", sourcePath, "error", err)
	}
}

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

// RebuildIndex rescans every page from disk and replaces the cached master index
// with a fresh one built from that scan, preserving LastProcessed.
//
// It holds writeMu across the whole scan + swap. That makes the disk state it
// reads a consistent snapshot: because every page writer
// (WritePage/UpdatePage/DeletePage/MarkSuperseded/MergePage/SplitPage/...)
// serializes on writeMu, none can land a file-write + index-update in the window
// between this scan and its swap. Without the lock, a write completing mid-scan
// would have its index entry silently dropped by the wholesale swap — the
// on-disk page stays correct, but the cached index goes transiently stale until
// the next write/rebuild self-heals it. That is the same "index agrees with
// disk" invariant writeMu exists to hold.
//
// The critical section spans reading all pages from disk, so it blocks other
// writers for the rebuild's duration. That tradeoff is acceptable here: the wiki
// is a single-user store of at most a few hundred small pages (tens of ms to
// parse), and RebuildIndex runs only from the background dream cycle, never the
// chat hot path. (The pre-existing swap already blocked all index readers under
// s.mu for the same instant; this only extends serialization to the rarer
// writers, and only while a background dream is mid-rebuild.)
func (s *Store) RebuildIndex() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	pages, err := s.ListPages("")
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	newIdx := NewIndex()
	for _, relPath := range pages {
		page, err := s.ReadPage(relPath)
		if err != nil {
			continue // unreadable/parse error: skip, leave it out of the index
		}
		newIdx.UpdateEntry(relPath, page)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Carry forward the diary high-water cursor from the index being replaced.
	newIdx.LastProcessed = s.index.LastProcessed
	s.index = newIdx
	return newIdx.Save(filepath.Join(s.dir, "index.md"))
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

// AppendDiary appends a timestamped entry to today's diary file and updates
// the in-memory diary search index so the new entry is immediately recallable.
// Safe to call from any goroutine.
//
// Callers that go through the package-level AppendDiaryTo (gmailpoll,
// morning_letter, etc.) bypass this indexing — their entries will only be
// searchable after the next gateway restart, when rebuildFromDir picks them
// up. Prefer Store.AppendDiary whenever a Store handle is available.
func (s *Store) AppendDiary(content string) error {
	if err := AppendDiaryTo(s.diaryDir, content); err != nil {
		return err
	}
	if s.diaryFTS != nil && content != "" {
		// Recreate the same (file, header, redacted-content, timestamp)
		// AppendDiaryTo just persisted, then push it into the index. Using
		// time.Now() once here can drift a microsecond from AppendDiaryTo,
		// but both round to the same HH:MM doc ID so the index is correct.
		now := time.Now()
		file := "diary-" + now.Format("2006-01-02") + ".md"
		header := now.Format("15:04")
		s.diaryFTS.upsertEntry(file, header, redact.String(content), now.UnixMilli())
	}
	return nil
}

// SearchDiary runs a full-text query over indexed diary entries, returning
// recency-weighted hits sorted best-first. Returns nil if no diary store is
// configured or the query is empty.
func (s *Store) SearchDiary(ctx context.Context, query string, limit int) ([]DiaryHit, error) {
	if s.diaryFTS == nil {
		return nil, nil
	}
	return s.diaryFTS.search(ctx, query, limit)
}

// RecentDiaryEntries returns the N most recent diary entries regardless of
// any query. Used as a fallback when the user's recall cue has no specific
// signal terms.
func (s *Store) RecentDiaryEntries(limit int) []DiaryHit {
	if s.diaryFTS == nil {
		return nil
	}
	return s.diaryFTS.recentEntries(limit)
}

// AppendDiaryTo appends a timestamped entry to today's diary file in the given directory.
// Standalone function usable without a Store instance.
//
// Diary content is the main input fed to the Wiki Dreamer, so any secret that
// makes it in here will later be paraphrased into synthesized wiki pages.
// Redacting at the write boundary closes that leak path at its source.
func AppendDiaryTo(diaryDir, content string) error {
	if content == "" || diaryDir == "" {
		return nil
	}
	content = redact.String(content)
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
//
// The details string often echoes page titles or user-provided content, so it
// is redacted before persistence for the same reason WritePageFile redacts the
// page body.
func (s *Store) AppendLog(operation, details string) error {
	details = redact.String(details)
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

// Close stops the background semantic refresh (waiting for any in-flight
// re-embed so its cache write cannot land after teardown) and releases the FTS
// search database.
func (s *Store) Close() error {
	if s.sem != nil {
		s.sem.shutdown()
	}
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
