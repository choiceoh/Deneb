// restructure.go — one-shot migration of the wiki onto the standardized
// per-project layout (see project_layout.go). Driven by cmd/wiki-restructure.
//
// The migration runs in two phases sharing one decision path:
//
//	plan:  read-only. Snapshot every page, apply the operator plan ops and the
//	       rule passes against an in-memory path set, and emit an ordered
//	       action list plus skip reasons.
//	apply: execute that exact action list through the Store primitives
//	       (MergePage/MovePage/DeletePage), then RebuildIndex.
//
// The gateway must be STOPPED while applying — Store locking is in-process
// only, and the live gateway additionally holds in-memory FTS/index state that
// direct disk mutation would desynchronize.
package wiki

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RestructureOp is one operator-authored judgment operation, applied in order
// before the rule passes. Fields by op:
//
//	merge:    Source page is folded into Target page (body appended under a
//	          "## 병합:" section) and deleted; references repoint to Target.
//	move:     Source page relocates to Target path; references repoint.
//	delete:   Source page is removed (junk/meta pages).
//	fold-log: Source page's body is appended to Target *project*'s 로그.md
//	          (created when absent) as a dated section, then Source is deleted.
type RestructureOp struct {
	Op     string `json:"op"`
	Source string `json:"source"`
	Target string `json:"target,omitempty"` // merge/move: page path; fold-log: project name
	Note   string `json:"note,omitempty"`
}

// restructureAction is one executable step of the finalized migration.
type restructureAction struct {
	kind       string // "merge" | "move" | "delete" | "ensure-log"
	source     string
	target     string
	mergedBody string // merge only
	reason     string
}

// RestructureReport summarizes a planned or applied migration.
type RestructureReport struct {
	Actions []string // human-readable, in execution order
	Skipped []string // decisions the rules refused to make (need a plan op)
	Applied bool
	Merged  int
	Moved   int
	Deleted int
	Errors  []string
}

// gmailIDRe matches a bare Gmail message-ID filename stem ("19e8717314b5c914").
var gmailIDRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// RestructureProjectLayout migrates the wiki onto the per-project layout.
// When apply is false only the report is produced; nothing is written.
func RestructureProjectLayout(store *Store, plan []RestructureOp, apply bool) (*RestructureReport, error) {
	if store == nil {
		return nil, fmt.Errorf("wiki: restructure needs a store")
	}
	rep := &RestructureReport{}

	// ---- snapshot ---------------------------------------------------------
	paths, err := store.ListPages("")
	if err != nil {
		return nil, fmt.Errorf("wiki: restructure list pages: %w", err)
	}
	sort.Strings(paths)
	pages := make(map[string]*Page, len(paths))
	exists := make(map[string]bool, len(paths))
	for _, p := range paths {
		p = strings.ReplaceAll(p, "\\", "/")
		page, perr := store.ReadPage(p)
		if perr != nil || page == nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("unreadable page: %s", p))
			continue
		}
		pages[p] = page
		exists[p] = true
	}

	var actions []restructureAction
	today := time.Now().Format("2006-01-02")

	// Simulated mutations keep later decisions consistent with earlier actions.
	simMove := func(src, dst string) {
		exists[src] = false
		exists[dst] = true
		pages[dst] = pages[src]
		delete(pages, src)
	}
	simDelete := func(src string) {
		exists[src] = false
		delete(pages, src)
	}

	// ---- phase 1: operator plan ops ---------------------------------------
	for i, op := range plan {
		src := normalizePagePath(strings.TrimSpace(op.Source))
		if src == "" || !exists[src] {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("plan[%d] %s: source missing: %s", i, op.Op, op.Source))
			continue
		}
		switch op.Op {
		case "merge":
			dst := normalizePagePath(strings.TrimSpace(op.Target))
			if dst == "" || !exists[dst] {
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("plan[%d] merge: target missing: %s", i, op.Target))
				continue
			}
			actions = append(actions, restructureAction{
				kind: "merge", source: src, target: dst,
				mergedBody: mergedBodyFor(pages[dst], pages[src]),
				reason:     planReason(op),
			})
			simDelete(src)
		case "move":
			dst := normalizePagePath(strings.TrimSpace(op.Target))
			if dst == "" || exists[dst] {
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("plan[%d] move: bad/occupied target: %s", i, op.Target))
				continue
			}
			actions = append(actions, restructureAction{kind: "move", source: src, target: dst, reason: planReason(op)})
			simMove(src, dst)
		case "delete":
			actions = append(actions, restructureAction{kind: "delete", source: src, reason: planReason(op)})
			simDelete(src)
		case "fold-log":
			project := strings.TrimSpace(op.Target)
			if project == "" {
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("plan[%d] fold-log: empty project", i))
				continue
			}
			logPath := LogPagePath(project)
			if !exists[logPath] {
				actions = append(actions, restructureAction{kind: "ensure-log", target: logPath, reason: "진행 로그 생성: " + project})
				exists[logPath] = true
				pages[logPath] = newLogPage(project)
			}
			srcPage := pages[src]
			heading := fmt.Sprintf("## %s %s", pageDateOr(srcPage, today), strings.TrimSpace(srcPage.Meta.Title))
			actions = append(actions, restructureAction{
				kind: "merge", source: src, target: logPath,
				mergedBody: strings.TrimSpace(pages[logPath].Body) + "\n\n" + heading + "\n\n" + strings.TrimSpace(srcPage.Body),
				reason:     planReason(op),
			})
			pages[logPath].Body = strings.TrimSpace(pages[logPath].Body) + "\n\n" + heading + "\n\n" + strings.TrimSpace(srcPage.Body)
			simDelete(src)
		default:
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("plan[%d]: unknown op %q", i, op.Op))
		}
	}

	// ---- phase 2: mail-analysis relocation (rule-based) --------------------
	projectNames := knownProjectNameSet(exists)
	for _, p := range sortedKeys(pages) {
		if !exists[p] {
			continue
		}
		page := pages[p]
		if !isMailAnalysisArtifact(p, page) {
			continue
		}
		project := resolveMailProject(p, page, projectNames)
		dst := MailAnalysisPagePath(project, strings.TrimSuffix(path.Base(p), ".md"))
		if dst == p {
			continue // already in place
		}
		if exists[dst] {
			if samePageContent(page, pages[dst]) {
				actions = append(actions, restructureAction{kind: "delete", source: p, reason: "동일 내용 중복 (이미 " + dst + " 존재)"})
				simDelete(p)
			} else {
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("mail-analysis collision (내용 상이): %s vs %s", p, dst))
			}
			continue
		}
		actions = append(actions, restructureAction{kind: "move", source: p, target: dst, reason: "메일분석 슬롯 정리"})
		simMove(p, dst)
	}

	// ---- phase 3: legacy flat 대표페이지 → in-folder slot -------------------
	for _, p := range sortedKeys(pages) {
		if !exists[p] {
			continue
		}
		name, ok := ProjectNameOf(p)
		if !ok || len(splitProjectPath(p)) != 1 {
			continue // not a flat project page
		}
		dst := RepPagePath(name)
		if exists[dst] {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("flat page needs merge decision (대표.md 이미 존재): %s vs %s", p, dst))
			continue
		}
		actions = append(actions, restructureAction{kind: "move", source: p, target: dst, reason: "대표페이지 폴더 이관"})
		simMove(p, dst)
	}

	// ---- phase 4: project folders missing a 대표페이지 get a minimal one ------
	// Without this, a folder-only project (all 39 production folders predate rep
	// pages) would vanish from KnownProjects — candidates, digests, research.
	// The dreamer/research cycles fill the skeleton afterwards.
	folderHasRep := make(map[string]bool)
	folderHasAny := make(map[string]bool)
	for p, ok := range exists {
		if !ok {
			continue
		}
		name, has := ProjectNameOf(p)
		if !has || len(splitProjectPath(p)) < 2 {
			continue
		}
		folderHasAny[name] = true
		if IsProjectRepPage(p) {
			folderHasRep[name] = true
		}
	}
	for _, name := range sortedBoolKeys(folderHasAny) {
		if folderHasRep[name] {
			continue
		}
		repPath := RepPagePath(name)
		actions = append(actions, restructureAction{kind: "ensure-rep", target: repPath, reason: "대표페이지 없는 프로젝트 폴더: " + name})
		exists[repPath] = true
		pages[repPath] = newRepPage(name)
	}

	// ---- render report ------------------------------------------------------
	for _, a := range actions {
		switch a.kind {
		case "merge":
			rep.Actions = append(rep.Actions, fmt.Sprintf("merge  %s → %s (%s)", a.source, a.target, a.reason))
		case "move":
			rep.Actions = append(rep.Actions, fmt.Sprintf("move   %s → %s (%s)", a.source, a.target, a.reason))
		case "delete":
			rep.Actions = append(rep.Actions, fmt.Sprintf("delete %s (%s)", a.source, a.reason))
		case "ensure-log", "ensure-rep":
			rep.Actions = append(rep.Actions, fmt.Sprintf("create %s (%s)", a.target, a.reason))
		}
	}
	if !apply {
		return rep, nil
	}

	// ---- execute ------------------------------------------------------------
	for _, a := range actions {
		var err error
		switch a.kind {
		case "merge":
			_, err = store.MergePage(a.target, a.source, a.mergedBody, MergeOptions{})
			if err == nil {
				rep.Merged++
			}
		case "move":
			err = store.MovePage(a.source, a.target)
			if err == nil {
				rep.Moved++
			}
		case "delete":
			err = store.DeletePage(a.source)
			if err == nil {
				rep.Deleted++
			}
		case "ensure-log":
			project, _ := ProjectNameOf(a.target)
			err = store.UpdatePage(a.target, func(existing *Page) (*Page, error) {
				if existing != nil {
					return nil, nil // already there — no-op
				}
				return newLogPage(project), nil
			})
		case "ensure-rep":
			project, _ := ProjectNameOf(a.target)
			err = store.UpdatePage(a.target, func(existing *Page) (*Page, error) {
				if existing != nil {
					return nil, nil // already there — no-op
				}
				return newRepPage(project), nil
			})
		}
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s %s: %v", a.kind, a.source, err))
		}
	}
	rep.Applied = true
	removeEmptyDirs(store.Dir())
	if err := store.RebuildIndex(); err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("rebuild index: %v", err))
	}
	return rep, nil
}

// removeEmptyDirs prunes directories the moves emptied (legacy mail-analyses/
// nests, typo buckets), deepest-first. The six category roots and the wiki root
// stay. Best-effort — a non-empty dir simply fails os.Remove and is kept.
func removeEmptyDirs(root string) {
	var dirs []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() || p == root {
			return nil //nolint:nilerr // best-effort walk
		}
		if base := filepath.Base(p); base == ".git" {
			return filepath.SkipDir
		}
		dirs = append(dirs, p)
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) }) // deepest first
	category := make(map[string]bool, len(Categories))
	for _, c := range Categories {
		category[filepath.Join(root, c)] = true
	}
	for _, d := range dirs {
		if category[d] {
			continue
		}
		_ = os.Remove(d) // fails (kept) unless empty
	}
}

// planReason renders a plan op's audit label.
func planReason(op RestructureOp) string {
	if strings.TrimSpace(op.Note) != "" {
		return "plan: " + op.Note
	}
	return "plan"
}

// mergedBodyFor renders the merged body for a plan merge: target body first,
// then the source under a dated "## 병합:" section so provenance stays visible.
func mergedBodyFor(target, source *Page) string {
	t := strings.TrimSpace(target.Body)
	s := strings.TrimSpace(source.Body)
	if s == "" {
		return t
	}
	head := fmt.Sprintf("## 병합: %s (%s)", strings.TrimSpace(source.Meta.Title), pageDateOr(source, ""))
	if t == "" {
		return head + "\n\n" + s
	}
	return t + "\n\n" + head + "\n\n" + s
}

// newRepPage mints a minimal 대표페이지 skeleton for a folder-only project; the
// dream/research cycles fill in the substance.
func newRepPage(project string) *Page {
	page := NewPage(project, projectCategoryPrefix, nil)
	page.Meta.Type = "project"
	page.Meta.Summary = project + " 프로젝트 대표페이지"
	page.Meta.Importance = 0.5
	page.Body = "# " + project + "\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- " +
		time.Now().Format("2006-01-02") + ": 레이아웃 이관으로 생성 (드림 사이클이 채움)\n"
	return page
}

// sortedBoolKeys returns a bool-set's keys sorted for deterministic ordering.
func sortedBoolKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// newLogPage mints a project's 로그.md skeleton.
func newLogPage(project string) *Page {
	page := NewPage(project+" 진행 로그", projectCategoryPrefix, nil)
	page.Meta.Type = "log"
	page.Meta.Summary = project + " 프로젝트 진행 이력 (사건·회의·결재 시간순 로그)"
	page.Meta.Importance = 0.4
	page.Body = "# " + project + " 진행 로그\n"
	return page
}

// pageDateOr returns the page's Updated (else Created) date, or fallback.
func pageDateOr(p *Page, fallback string) string {
	if d := strings.TrimSpace(p.Meta.Updated); d != "" {
		return d
	}
	if d := strings.TrimSpace(p.Meta.Created); d != "" {
		return d
	}
	return fallback
}

// isMailAnalysisArtifact reports whether a page is an auto-generated per-mail
// analysis page, by path shape (any 메일분석/mail-analyses bucket), filename
// (bare Gmail hex ID), or content markers the mail sink writes.
func isMailAnalysisArtifact(relPath string, page *Page) bool {
	if IsMailAnalysisPath(relPath) {
		return true
	}
	stem := strings.TrimSuffix(path.Base(relPath), ".md")
	if gmailIDRe.MatchString(stem) {
		return true
	}
	if strings.TrimSpace(page.Meta.Category) == "mail-analysis" {
		return true
	}
	if strings.HasSuffix(strings.TrimSpace(page.Meta.Summary), "메일 분석") &&
		strings.Contains(page.Body, "> Message ID: `") {
		return true
	}
	return false
}

// resolveMailProject picks the owning project for a mail-analysis page: the
// project folder it already sits in, the mail-analyses/<sub>/ folder name when
// it names a known project, then the first Related entry resolving to a project
// 대표페이지. Empty = the category-level unlinked bucket.
func resolveMailProject(relPath string, page *Page, projectNames map[string]bool) string {
	if name, ok := ProjectNameOf(relPath); ok && projectNames[name] {
		return name
	}
	seg := splitProjectPath(relPath)
	if len(seg) >= 3 && (seg[0] == legacyMailAnalysisDir || seg[0] == MailAnalysisDir) && projectNames[seg[1]] {
		return seg[1]
	}
	for _, r := range page.Meta.Related {
		r = strings.TrimSpace(r)
		if !IsProjectRepPage(r) {
			continue
		}
		if name, ok := ProjectNameOf(r); ok && projectNames[name] {
			return name
		}
	}
	return ""
}

// knownProjectNameSet derives the project-name universe from the simulated path
// set: project folder names plus legacy flat page basenames (reserved buckets
// excluded).
func knownProjectNameSet(exists map[string]bool) map[string]bool {
	names := make(map[string]bool)
	for p, ok := range exists {
		if !ok {
			continue
		}
		if name, has := ProjectNameOf(p); has {
			names[name] = true
		}
	}
	return names
}

// samePageContent reports whether two pages carry the same body (whitespace
// trimmed) — the identical-duplicate test for collision handling.
func samePageContent(a, b *Page) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.TrimSpace(a.Body) == strings.TrimSpace(b.Body)
}

// sortedKeys returns map keys sorted for deterministic action ordering.
func sortedKeys(m map[string]*Page) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
