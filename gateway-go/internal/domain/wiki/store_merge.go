// store_merge.go — page lifecycle beyond simple CRUD: supersession stamps
// (MarkSuperseded) and the duplicate-page merge pass (MergePage), including
// reference repointing and frontmatter/related-list union helpers. Split
// from store.go (Store core).
package wiki

import (
	"fmt"
	"strings"
	"time"
)

// MarkSuperseded stamps oldPath as replaced by newPath: the page stays
// readable (history is memory too) but search demotes it so the stale fact
// stops surfacing as current. Idempotent; refuses self-supersession.
//
// Holds writeMu so the read-modify-write of oldPath's frontmatter is atomic
// against a concurrent writer of the same page.
func (s *Store) MarkSuperseded(oldPath, newPath string) error {
	oldPath = normalizePagePath(oldPath)
	newPath = normalizePagePath(newPath)
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return nil
	}
	if err := supersedePrecondition(oldPath, newPath); err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	page, err := s.ReadPage(oldPath)
	if err != nil {
		return fmt.Errorf("wiki: mark superseded: %w", err)
	}
	if page.Meta.SupersededBy == newPath {
		return nil
	}
	page.Meta.SupersededBy = newPath
	page.Meta.Updated = time.Now().Format("2006-01-02")
	return s.writePageInternal(oldPath, page, true)
}

// supersedePrecondition refuses the supersession relationships that are never
// "the same topic's newer version" — the only meaning the flag has.
//
// superseded_by is consumed as retirement: search multiplies a superseded page's
// score by 0.15, the counterparty anchor drops it, and verify auto-archives it
// 30 days later. So a wrong flag silently deletes knowledge from recall. Both
// misuses below happened on the live wiki: a project's own 로그.md was marked
// superseded by its 대표.md (that log becomes recall-dead 30 days on), and a
// counterparty 거래 ledger plus two mail analyses were marked superseded by a
// project rep page — mail is raw evidence and a ledger is a different document
// type, neither is a stale version of anything (the same misuse killed recall
// for 부산8호 by recording business succession as knowledge replacement).
//
// Layout slots and raw data are therefore never the OLD side of a supersession.
// Ordinary curated pages (기타/업무/인물 …) still supersede freely.
func supersedePrecondition(oldPath, newPath string) error {
	// The legacy flat form 프로젝트/<name>.md also answers IsProjectRepPage, but it
	// is an ordinary curated page in practice (and the migration's leftover), so
	// only in-folder slots are guarded.
	_, inFolder := inFolderProjectOf(oldPath)
	switch {
	case inFolder && IsProjectRepPage(oldPath):
		return fmt.Errorf("wiki: refusing to supersede a 대표페이지 (%s → %s) — 프로젝트 승계는 종결/related로 기록", oldPath, newPath)
	case IsProjectLogPage(oldPath):
		return fmt.Errorf("wiki: refusing to supersede a 로그.md (%s → %s) — 진행 로그는 대체되지 않는다", oldPath, newPath)
	case IsMailAnalysisPath(oldPath):
		return fmt.Errorf("wiki: refusing to supersede a 메일분석 page (%s → %s) — 메일은 원시 증거", oldPath, newPath)
	case IsDealLedgerPath(oldPath):
		return fmt.Errorf("wiki: refusing to supersede a 거래 원장 (%s → %s) — 원장은 프로젝트 문서로 대체되지 않는다", oldPath, newPath)
	}
	return nil
}

func toSet(ss []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ss))
	for _, s := range ss {
		m[s] = struct{}{}
	}
	return m
}

// MergeOptions reserves room for future merge-policy knobs. v1 uses fixed
// defaults (see MergePage), so the struct is currently empty.
type MergeOptions struct{}

// MergeResult summarizes a completed page merge.
type MergeResult struct {
	TargetPath    string // the surviving page
	MergedTitle   string // target's title after the merge
	RewriteCount  int    // other pages whose Related was repointed source→target
	SourceRemoved bool   // whether the source page was deleted
}

// MergePage folds sourcePath into targetPath and deletes source. The target
// keeps its identity (title, category, path) and takes mergedBody as its new
// body; the two pages' frontmatter is unioned (see mergeFrontmatterInto); every
// other page that referenced source is repointed to target; and source is
// removed from disk, the master index, and the search index.
//
// mergedBody is supplied by the caller (LLM-synthesized, or a plain
// concatenation fallback) so this method carries no LLM dependency — the domain
// layer stays pure.
//
// Ordering is deliberate for crash-safety: target is written first (so the
// merged body is durable before anything is destroyed), references are
// repointed next, and source is deleted last. A failure partway through leaves
// source intact and the merge re-runnable, with no data loss.
//
// Backlinks are managed manually here (every write passes skipBacklinks=true)
// because the merge already rewrites both link directions itself; letting
// maintainBacklinks also fire would double-process the same edges.
func (s *Store) MergePage(targetPath, sourcePath, mergedBody string, _ MergeOptions) (MergeResult, error) {
	targetPath = strings.TrimSpace(targetPath)
	sourcePath = strings.TrimSpace(sourcePath)
	if targetPath == "" || sourcePath == "" {
		return MergeResult{}, fmt.Errorf("wiki: merge needs both target and source paths")
	}
	// Compare NORMALIZED identities: "기타/dup" and "기타/dup.md" are the same
	// file, and a raw-string guard let that spelling variant through — the
	// "merge" then deleted the page it had just written (self-merge data loss).
	targetPath = normalizePagePath(targetPath)
	sourcePath = normalizePagePath(sourcePath)
	if targetPath == sourcePath {
		return MergeResult{}, fmt.Errorf("wiki: cannot merge a page into itself")
	}

	// Hold writeMu across the whole merge (read both pages, write target, repoint
	// references, delete source) so no concurrent writer slips an edit into either
	// page mid-merge. The internal helpers (writePageInternal, repointReference,
	// deletePageLocked) all assume the lock is held.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	return s.mergePageLocked(targetPath, sourcePath, func(target, source *Page) string {
		// Caller-supplied merged text. Guard against an empty body silently
		// wiping content: fall back to a plain concatenation.
		if strings.TrimSpace(mergedBody) != "" {
			return mergedBody
		}
		return strings.TrimSpace(target.Body + "\n\n" + source.Body)
	})
}

// mergePageLocked is the shared body of MergePage and FoldDuplicate: read both
// pages, synthesize the surviving body via bodyFn (called with the pages as
// read UNDER the lock, so no mid-window write can be lost), union frontmatter,
// repoint inbound references, delete the source. The caller must hold writeMu
// and pass normalized, non-identical paths.
func (s *Store) mergePageLocked(targetPath, sourcePath string, bodyFn func(target, source *Page) string) (MergeResult, error) {
	target, err := s.ReadPage(targetPath)
	if err != nil {
		return MergeResult{}, fmt.Errorf("wiki: read merge target %q: %w", targetPath, err)
	}
	source, err := s.ReadPage(sourcePath)
	if err != nil {
		return MergeResult{}, fmt.Errorf("wiki: read merge source %q: %w", sourcePath, err)
	}

	// Collect every page that references source. The index scan is the source
	// of truth (robust against backlink-mirror drift); source's own Related is
	// folded in too in case the index lags. target/source/blank are excluded.
	refSet := make(map[string]struct{})
	for _, p := range s.findPagesReferencingPath(sourcePath) {
		refSet[p] = struct{}{}
	}
	for _, r := range source.Meta.Related {
		refSet[strings.TrimSpace(r)] = struct{}{}
	}
	delete(refSet, targetPath)
	delete(refSet, sourcePath)
	delete(refSet, "")

	// 1. Body — synthesized from the locked reads.
	target.Body = bodyFn(target, source)

	// 2. Frontmatter union (tags, importance, due, summary, created).
	mergeFrontmatterInto(&target.Meta, source.Meta)

	// 3. Related = target's ∪ source's ∪ referencing-pages, minus self+source.
	exclude := map[string]struct{}{targetPath: {}, sourcePath: {}}
	target.Meta.Related = unionRelated(exclude,
		target.Meta.Related, source.Meta.Related, keysOf(refSet))
	target.Meta.Updated = time.Now().Format("2006-01-02")

	// 4. Write target first (manual backlinks → skip auto maintenance).
	if err := s.writePageInternal(targetPath, target, true); err != nil {
		return MergeResult{}, fmt.Errorf("wiki: write merge target: %w", err)
	}

	// 5. Repoint each referencing page: source → target.
	rewrites := 0
	for p := range refSet {
		if s.repointReference(p, sourcePath, targetPath) {
			rewrites++
		}
	}

	// 6. Delete source last. Its neighbors were already repointed above, so
	//    the backlink cleanup is a harmless no-op. deletePageLocked (not the
	//    public DeletePage) because we already hold writeMu.
	if err := s.deletePageLocked(sourcePath); err != nil {
		return MergeResult{TargetPath: targetPath, MergedTitle: target.Meta.Title, RewriteCount: rewrites},
			fmt.Errorf("wiki: delete merge source: %w", err)
	}

	_ = s.appendLog("merge", targetPath+" ← "+sourcePath+" — "+target.Meta.Title) // best-effort: audit log is non-critical

	return MergeResult{
		TargetPath:    targetPath,
		MergedTitle:   target.Meta.Title,
		RewriteCount:  rewrites,
		SourceRemoved: true,
	}, nil
}

// findPagesReferencingPath scans the master index for every page (other than
// relPath itself) whose Related list contains relPath. Index-based so it sees
// all inbound references regardless of any backlink-mirror drift. Matching is
// on normalized paths — a related entry written without the ".md" extension
// still counts as an inbound reference.
func (s *Store) findPagesReferencingPath(relPath string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	want := normalizePagePath(relPath)
	var out []string
	for path, entry := range s.index.Entries {
		if path == want {
			continue
		}
		for _, r := range entry.Related {
			if normalizePagePath(r) == want {
				out = append(out, path)
				break
			}
		}
	}
	return out
}

// repointReference rewrites oldRef→newRef in relPath's Related list (dedup,
// dropping any self-reference) and persists with skipBacklinks=true. Returns
// true if the page changed. Best-effort: an unreadable page is skipped.
func (s *Store) repointReference(relPath, oldRef, newRef string) bool {
	page, err := s.ReadPage(relPath)
	if err != nil {
		return false
	}
	// Normalized matching throughout: a ref recorded as "기타/x" must repoint
	// (and dedupe) the same as "기타/x.md".
	oldNorm := normalizePagePath(oldRef)
	selfNorm := normalizePagePath(relPath)
	seen := make(map[string]struct{}, len(page.Meta.Related))
	rebuilt := make([]string, 0, len(page.Meta.Related))
	changed := false
	for _, r := range page.Meta.Related {
		if normalizePagePath(r) == oldNorm {
			r = newRef
			changed = true
		}
		if normalizePagePath(r) == selfNorm { // never self-reference
			changed = true
			continue
		}
		if _, dup := seen[normalizePagePath(r)]; dup {
			continue
		}
		seen[normalizePagePath(r)] = struct{}{}
		rebuilt = append(rebuilt, r)
	}
	if !changed {
		return false
	}
	page.Meta.Related = rebuilt
	// No Updated stamp — see addBacklink: a Related repoint is metadata hygiene
	// on the referencing page, not page activity. Stamping here let one merge or
	// move (or a wiki-restructure batch of them) reset the dormancy clock of
	// every page that merely referenced the moved one.
	_ = s.writePageInternal(relPath, page, true) // best-effort: reference repoint is non-critical
	return true
}

// mergeFrontmatterInto folds src's frontmatter into dst while keeping dst's
// identity. Tags, cue anchors, emails, sites, and kinds become a union; PID
// and client fill in from src only when dst's is empty; importance takes the
// max; due/created take the earlier date (a merged entity's history starts at
// the earliest); summary, the frozen project code, and the resource URI fill
// in from src only when dst's is empty (the code is the move-stable project
// identity — dropping it on a legacy-flat merge kills every code-form related
// edge into the surviving page). Title, Category, Type, Confidence, Archived,
// and ID stay dst's.
func mergeFrontmatterInto(dst *Frontmatter, src Frontmatter) {
	dst.Tags = unionStrings(dst.Tags, src.Tags)
	// Cue anchors are recall entry points — dropping the source page's cues on a
	// duplicate-fold would silently kill the paraphrase queries that page
	// answered. Union with dst priority; normalizeCues dedupes/trims/caps.
	dst.Cues = normalizeCues(append(append([]string{}, dst.Cues...), src.Cues...))
	// Emails, sites, and kinds are identity/anchor keys the fold must not drop
	// with the deleted source: emails are the canonical 동명이인 disambiguation
	// key, sites/kinds the project's 현장/특성 recall-anchor + digest-grouping
	// keys. Union so the survivor keeps both pages' values (normalize dedupes).
	dst.Emails = unionStrings(dst.Emails, src.Emails)
	dst.Sites = normalizeSites(append(append([]string{}, dst.Sites...), src.Sites...))
	dst.Kinds = normalizeKinds(append(append([]string{}, dst.Kinds...), src.Kinds...))
	// Episode provenance must survive the fold: the source page is deleted after
	// its body merges into the survivor, so dropping its refs would leave the
	// merged facts uncitable in the graph. Union (bounded newest-window).
	dst.Sources = normalizeSources(append(append([]string{}, dst.Sources...), src.Sources...))
	if src.Importance > dst.Importance {
		dst.Importance = src.Importance
	}
	if strings.TrimSpace(dst.Summary) == "" {
		dst.Summary = src.Summary
	}
	if strings.TrimSpace(dst.Code) == "" {
		dst.Code = src.Code
	}
	// PID is the move-stable, frozen person identity (the person-only analogue of
	// Code); client is fill-only per the wiki-layout rule (기존 값 덮어쓰기 금지).
	if strings.TrimSpace(dst.PID) == "" {
		dst.PID = src.PID
	}
	if strings.TrimSpace(dst.Client) == "" {
		dst.Client = src.Client
	}
	if strings.TrimSpace(dst.Resource) == "" {
		dst.Resource = src.Resource
	}
	dst.Due = earlierDate(dst.Due, src.Due)
	dst.Created = earlierDate(dst.Created, src.Created)
}

// keysOf returns the keys of a set in arbitrary order.
func keysOf(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// unionRelated concatenates related-path lists in order, dropping blanks,
// excluded paths, and duplicates (first occurrence wins).
func unionRelated(exclude map[string]struct{}, lists ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, list := range lists {
		for _, r := range list {
			r = strings.TrimSpace(r)
			if r == "" {
				continue
			}
			if _, ex := exclude[r]; ex {
				continue
			}
			if _, dup := seen[r]; dup {
				continue
			}
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}

// unionStrings merges two string slices preserving a-first order, trimming
// blanks and removing duplicates.
func unionStrings(a, b []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, list := range [][]string{a, b} {
		for _, v := range list {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

// earlierDate returns the lexicographically smaller of two YYYY-MM-DD strings
// (ISO dates sort chronologically), ignoring empties.
func earlierDate(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	case a <= b:
		return a
	default:
		return b
	}
}

// ListPages returns all page paths in a category (e.g., "업무").
// If category is empty, returns all pages.
