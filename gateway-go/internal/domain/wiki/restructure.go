// restructure.go — one-shot migration of the wiki onto the standardized
// per-project layout (see project_layout.go). Driven by cmd/wiki-restructure.
//
// The migration runs in two phases sharing one decision path:
//
//	plan:  read-only. Snapshot every page, apply the operator plan ops and the
//	       rule passes against an in-memory path set, and emit an ordered
//	       action list plus skip reasons.
//	apply: execute that exact action list through the Store primitives
//	       (MergePage/MovePage/DeletePage), then rebuildIndex.
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
//	merge:      Source page is folded into Target page (body appended under a
//	            "## 병합:" section) and deleted; references repoint to Target.
//	move:       Source page relocates to Target path; references repoint.
//	delete:     Source page is removed (junk/meta pages).
//	fold-log:   Source page's body is appended to Target *project*'s 로그.md
//	            (created when absent) as a dated section, then Source is deleted.
//	set-client: Source 대표페이지's frontmatter client (거래처) is set to Target
//	            (a canonical single-level 계열사 name; empty Target clears it).
//	            Metadata-only — Updated is NOT re-stamped (a classification
//	            backfill is not page activity).
type RestructureOp struct {
	Op     string `json:"op"`
	Source string `json:"source"`
	Target string `json:"target,omitempty"` // merge/move: page path; fold-log: project name
	Note   string `json:"note,omitempty"`
}

// restructureAction is one executable step of the finalized migration.
type restructureAction struct {
	kind       string // "merge" | "move" | "delete" | "ensure-log" | "set-client"
	source     string
	target     string // set-client: the 거래처 value, not a path
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
	state, err := loadRestructurePlanningState(store, rep)
	if err != nil {
		return nil, err
	}

	// ---- phase 1: operator plan ops ---------------------------------------
	state.applyOperatorPlan(plan)

	// ---- phase 2: mail-analysis relocation (rule-based) --------------------
	state.planMailAnalysisRelocation()

	// ---- phase 3: legacy flat 대표페이지 → in-folder slot -------------------
	state.planFlatRepresentativeMoves()

	// ---- phase 4: project folders missing a 대표페이지 get a minimal one ------
	// Without this, a folder-only project (all 39 production folders predate rep
	// pages) would vanish from KnownProjects — candidates, digests, research.
	// The dreamer/research cycles fill the skeleton afterwards.
	state.planMissingRepresentatives()

	// ---- render report ------------------------------------------------------
	rep.Actions = renderRestructureActions(state.actions)
	if !apply {
		return rep, nil
	}

	// ---- execute ------------------------------------------------------------
	executeRestructureActions(store, state.actions, rep)
	rep.Applied = true
	removeEmptyDirs(store.Dir())
	if err := store.rebuildIndex(); err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("rebuild index: %v", err))
	}
	return rep, nil
}

// restructurePlanningState is the in-memory migration simulation shared by all
// planning phases. Each phase mutates this snapshot so later decisions observe
// earlier moves, merges, deletes, and skeleton creation without touching disk.
type restructurePlanningState struct {
	pages   map[string]*Page
	exists  map[string]bool
	actions []restructureAction
	report  *RestructureReport
	today   string
}

func loadRestructurePlanningState(store *Store, report *RestructureReport) (*restructurePlanningState, error) {
	paths, err := store.ListPages("")
	if err != nil {
		return nil, fmt.Errorf("wiki: restructure list pages: %w", err)
	}
	sort.Strings(paths)
	state := &restructurePlanningState{
		pages:  make(map[string]*Page, len(paths)),
		exists: make(map[string]bool, len(paths)),
		report: report,
	}
	for _, relPath := range paths {
		relPath = strings.ReplaceAll(relPath, "\\", "/")
		page, readErr := store.ReadPage(relPath)
		if readErr != nil || page == nil {
			report.Skipped = append(report.Skipped, fmt.Sprintf("unreadable page: %s", relPath))
			continue
		}
		state.pages[relPath] = page
		state.exists[relPath] = true
	}
	state.today = time.Now().Format("2006-01-02")
	return state, nil
}

func (s *restructurePlanningState) simulateMove(source, target string) {
	s.exists[source] = false
	s.exists[target] = true
	s.pages[target] = s.pages[source]
	delete(s.pages, source)
}

func (s *restructurePlanningState) simulateDelete(source string) {
	s.exists[source] = false
	delete(s.pages, source)
}

func (s *restructurePlanningState) applyOperatorPlan(plan []RestructureOp) {
	for index, op := range plan {
		s.applyOperatorPlanOp(index, op)
	}
}

func (s *restructurePlanningState) applyOperatorPlanOp(index int, op RestructureOp) {
	source := normalizePagePath(strings.TrimSpace(op.Source))
	if source == "" || !s.exists[source] {
		s.report.Skipped = append(s.report.Skipped,
			fmt.Sprintf("plan[%d] %s: source missing: %s", index, op.Op, op.Source))
		return
	}
	switch op.Op {
	case "merge":
		s.planOperatorMerge(index, op, source)
	case "move":
		s.planOperatorMove(index, op, source)
	case "delete":
		s.actions = append(s.actions, restructureAction{kind: "delete", source: source, reason: planReason(op)})
		s.simulateDelete(source)
	case "set-client":
		s.planOperatorSetClient(index, op, source)
	case "fold-log":
		s.planOperatorFoldLog(index, op, source)
	default:
		s.report.Skipped = append(s.report.Skipped, fmt.Sprintf("plan[%d]: unknown op %q", index, op.Op))
	}
}

func (s *restructurePlanningState) planOperatorMerge(index int, op RestructureOp, source string) {
	target := normalizePagePath(strings.TrimSpace(op.Target))
	if target == "" || !s.exists[target] {
		s.report.Skipped = append(s.report.Skipped,
			fmt.Sprintf("plan[%d] merge: target missing: %s", index, op.Target))
		return
	}
	body := mergedBodyFor(s.pages[target], s.pages[source])
	s.actions = append(s.actions, restructureAction{
		kind: "merge", source: source, target: target,
		mergedBody: body,
		reason:     planReason(op),
	})
	// A later merge into the same target must include this earlier merge.
	s.pages[target].Body = body
	s.simulateDelete(source)
}

func (s *restructurePlanningState) planOperatorMove(index int, op RestructureOp, source string) {
	target := normalizePagePath(strings.TrimSpace(op.Target))
	if target == "" || s.exists[target] {
		s.report.Skipped = append(s.report.Skipped,
			fmt.Sprintf("plan[%d] move: bad/occupied target: %s", index, op.Target))
		return
	}
	s.actions = append(s.actions, restructureAction{
		kind: "move", source: source, target: target, reason: planReason(op),
	})
	s.simulateMove(source, target)
}

func (s *restructurePlanningState) planOperatorSetClient(index int, op RestructureOp, source string) {
	if !IsProjectRepPage(source) {
		s.report.Skipped = append(s.report.Skipped,
			fmt.Sprintf("plan[%d] set-client: not a 대표페이지: %s", index, op.Source))
		return
	}
	s.actions = append(s.actions, restructureAction{
		kind: "set-client", source: source, target: strings.TrimSpace(op.Target),
		reason: planReason(op),
	})
}

func (s *restructurePlanningState) planOperatorFoldLog(index int, op RestructureOp, source string) {
	project := strings.TrimSpace(op.Target)
	if project == "" {
		s.report.Skipped = append(s.report.Skipped, fmt.Sprintf("plan[%d] fold-log: empty project", index))
		return
	}
	logPath := LogPagePath(project)
	if !s.exists[logPath] {
		s.actions = append(s.actions, restructureAction{
			kind: "ensure-log", target: logPath, reason: "진행 로그 생성: " + project,
		})
		s.exists[logPath] = true
		s.pages[logPath] = newLogPage(project)
	}
	sourcePage := s.pages[source]
	heading := fmt.Sprintf("## %s %s", pageDateOr(sourcePage, s.today), strings.TrimSpace(sourcePage.Meta.Title))
	mergedBody := strings.TrimSpace(s.pages[logPath].Body) + "\n\n" + heading + "\n\n" + strings.TrimSpace(sourcePage.Body)
	s.actions = append(s.actions, restructureAction{
		kind: "merge", source: source, target: logPath,
		mergedBody: mergedBody,
		reason:     planReason(op),
	})
	s.pages[logPath].Body = mergedBody
	s.simulateDelete(source)
}

func (s *restructurePlanningState) planMailAnalysisRelocation() {
	projectNames := knownProjectNameSet(s.exists)
	for _, relPath := range sortedKeys(s.pages) {
		s.planMailAnalysisPage(relPath, projectNames)
	}
}

func (s *restructurePlanningState) planMailAnalysisPage(relPath string, projectNames map[string]bool) {
	if !s.exists[relPath] {
		return
	}
	page := s.pages[relPath]
	if !isMailAnalysisArtifact(relPath, page) {
		return
	}
	project := resolveMailProject(relPath, page, projectNames)
	target := MailAnalysisPagePath(project, strings.TrimSuffix(path.Base(relPath), ".md"))
	if target == relPath {
		return
	}
	if s.exists[target] {
		s.planMailAnalysisCollision(relPath, target, page)
		return
	}
	s.actions = append(s.actions, restructureAction{
		kind: "move", source: relPath, target: target, reason: "메일분석 슬롯 정리",
	})
	s.simulateMove(relPath, target)
}

func (s *restructurePlanningState) planMailAnalysisCollision(source, target string, page *Page) {
	if samePageContent(page, s.pages[target]) {
		s.actions = append(s.actions, restructureAction{
			kind: "delete", source: source, reason: "동일 내용 중복 (이미 " + target + " 존재)",
		})
		s.simulateDelete(source)
		return
	}
	s.report.Skipped = append(s.report.Skipped,
		fmt.Sprintf("mail-analysis collision (내용 상이): %s vs %s", source, target))
}

func (s *restructurePlanningState) planFlatRepresentativeMoves() {
	for _, relPath := range sortedKeys(s.pages) {
		s.planFlatRepresentativeMove(relPath)
	}
}

func (s *restructurePlanningState) planFlatRepresentativeMove(relPath string) {
	if !s.exists[relPath] {
		return
	}
	project, ok := ProjectNameOf(relPath)
	if !ok || len(splitProjectPath(relPath)) != 1 {
		return
	}
	target := RepPagePath(project)
	if s.exists[target] {
		s.report.Skipped = append(s.report.Skipped,
			fmt.Sprintf("flat page needs merge decision (대표.md 이미 존재): %s vs %s", relPath, target))
		return
	}
	s.actions = append(s.actions, restructureAction{
		kind: "move", source: relPath, target: target, reason: "대표페이지 폴더 이관",
	})
	s.simulateMove(relPath, target)
}

func (s *restructurePlanningState) planMissingRepresentatives() {
	folderHasAny, folderHasRep := s.projectFolderCoverage()
	for _, project := range sortedBoolKeys(folderHasAny) {
		if folderHasRep[project] {
			continue
		}
		target := RepPagePath(project)
		s.actions = append(s.actions, restructureAction{
			kind: "ensure-rep", target: target, reason: "대표페이지 없는 프로젝트 폴더: " + project,
		})
		s.exists[target] = true
		s.pages[target] = newRepPage(project)
	}
}

func (s *restructurePlanningState) projectFolderCoverage() (map[string]bool, map[string]bool) {
	folderHasAny := make(map[string]bool)
	folderHasRep := make(map[string]bool)
	for relPath, exists := range s.exists {
		if !exists {
			continue
		}
		project, ok := ProjectNameOf(relPath)
		if !ok || len(splitProjectPath(relPath)) < 2 {
			continue
		}
		folderHasAny[project] = true
		if IsProjectRepPage(relPath) {
			folderHasRep[project] = true
		}
	}
	return folderHasAny, folderHasRep
}

func renderRestructureActions(actions []restructureAction) []string {
	var rendered []string
	for _, action := range actions {
		switch action.kind {
		case "merge":
			rendered = append(rendered, fmt.Sprintf("merge  %s → %s (%s)", action.source, action.target, action.reason))
		case "move":
			rendered = append(rendered, fmt.Sprintf("move   %s → %s (%s)", action.source, action.target, action.reason))
		case "delete":
			rendered = append(rendered, fmt.Sprintf("delete %s (%s)", action.source, action.reason))
		case "ensure-log", "ensure-rep":
			rendered = append(rendered, fmt.Sprintf("create %s (%s)", action.target, action.reason))
		case "set-client":
			rendered = append(rendered, fmt.Sprintf("client %s ← %q (%s)", action.source, action.target, action.reason))
		}
	}
	return rendered
}

func executeRestructureActions(store *Store, actions []restructureAction, report *RestructureReport) {
	for _, action := range actions {
		if err := executeRestructureAction(store, action); err != nil {
			report.Errors = append(report.Errors,
				fmt.Sprintf("%s %s: %v", action.kind, action.source, err))
			continue
		}
		countAppliedRestructureAction(report, action.kind)
	}
}

func executeRestructureAction(store *Store, action restructureAction) error {
	switch action.kind {
	case "merge":
		_, err := store.MergePage(action.target, action.source, action.mergedBody, MergeOptions{})
		return err
	case "move":
		return store.MovePage(action.source, action.target)
	case "delete":
		return store.DeletePage(action.source)
	case "ensure-log":
		return ensureRestructureLogPage(store, action.target)
	case "ensure-rep":
		return ensureRestructureRepPage(store, action.target)
	case "set-client":
		return setRestructureClient(store, action.source, action.target)
	default:
		return nil
	}
}

func countAppliedRestructureAction(report *RestructureReport, kind string) {
	switch kind {
	case "merge":
		report.Merged++
	case "move":
		report.Moved++
	case "delete":
		report.Deleted++
	}
}

func ensureRestructureLogPage(store *Store, target string) error {
	project, _ := ProjectNameOf(target)
	return store.UpdatePage(target, func(existing *Page) (*Page, error) {
		if existing != nil {
			return nil, nil
		}
		return newLogPage(project), nil
	})
}

func ensureRestructureRepPage(store *Store, target string) error {
	project, _ := ProjectNameOf(target)
	return store.UpdatePage(target, func(existing *Page) (*Page, error) {
		if existing != nil {
			return nil, nil
		}
		return newRepPage(project), nil
	})
}

func setRestructureClient(store *Store, source, rawClient string) error {
	client := normalizeClientName(rawClient)
	return store.UpdatePage(source, func(existing *Page) (*Page, error) {
		if existing == nil {
			return nil, fmt.Errorf("대표페이지 없음: %s", source)
		}
		if existing.Meta.Client == client {
			return nil, nil
		}
		existing.Meta.Client = client
		return existing, nil
	})
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

// RepSkeletonMarker is stamped into 대표페이지 skeletons minted by the layout
// migration. The research task treats pages carrying it as backfill targets
// (batched per cycle until the fleet of empty rep pages is filled in).
const RepSkeletonMarker = "레이아웃 이관으로 생성 (연구·드림 사이클이 채움)"

// newRepPage mints a minimal 대표페이지 skeleton for a folder-only project; the
// dream/research cycles fill in the substance.
func newRepPage(project string) *Page {
	page := NewPage(project, projectCategoryPrefix, nil)
	page.Meta.Type = "project"
	page.Meta.Summary = project + " 프로젝트 대표페이지"
	page.Meta.Importance = 0.5
	page.Body = "# " + project + "\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- " +
		time.Now().Format("2006-01-02") + ": " + RepSkeletonMarker + "\n"
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
	if len(seg) >= 3 && (seg[0] == legacyMailAnalysisDir || seg[0] == mailAnalysisDir) && projectNames[seg[1]] {
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
