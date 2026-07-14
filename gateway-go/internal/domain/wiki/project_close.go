// project_close.go — project lifecycle: 종결(close) / 재개(reopen) / 졸음 감지.
//
// A closed project leaves the ACTIVE stage without losing anything: no file
// moves (path stability is the layout's core lesson), no deletion. Closure =
// a "## 종결" record on the 대표페이지 + the Archived flag on every page in the
// project folder. Because KnownProjects skips archived rep pages, one flag
// retires the project everywhere at once — mail-analyzer candidates, the
// 모아보기 digests, the research rotation, and the reviewer's code signal —
// while every page stays readable and searchable (demoted, not gone).
// Reopen reverses it wholesale.
package wiki

import (
	"fmt"
	"strings"
	"time"
)

// closedSectionHeading is the H2 section on a 대표페이지 recording closure.
const closedSectionHeading = "종결"

// dormantAfterDays is how long a project may sit with no page activity before
// the dormancy check proposes closure (advisory bullet, never auto-close).
const dormantAfterDays = 120

// closeResult summarizes a project closure.
type closeResult struct {
	RepPath  string // the 대표페이지 the 종결 record landed on
	Archived int    // pages newly flagged archived (rep included)
}

// reopenResult summarizes a project reopening.
type reopenResult struct {
	RepPath  string
	Restored int // pages un-archived
}

// resolveProjectRep maps a project reference (bare name, folder path, or any
// page path inside the project) to its 대표페이지 path — the in-folder 대표.md
// first, the legacy flat page as fallback. Unlike KnownProjects this looks at
// the disk directly, so it finds CLOSED (archived) projects too — reopen needs
// exactly that.
// ResolveProjectRep resolves a project reference (folder name, legacy flat
// name, rep-page path, or display title) to its canonical name and the rep
// page path that actually exists. Exported for write paths that must accept
// every rep form the transition-era wiki contains — e.g. ingest project
// linking, where validating only the folder form silently dropped legacy
// flat projects into the global bucket.
func (s *Store) ResolveProjectRep(ref string) (name, repPath string, err error) {
	return s.resolveProjectRep(ref)
}

func (s *Store) resolveProjectRep(ref string) (name, repPath string, err error) {
	ref = strings.ReplaceAll(strings.TrimSpace(ref), "\\", "/")
	if ref == "" {
		return "", "", fmt.Errorf("wiki: empty project reference")
	}
	if n, ok := ProjectNameOf(ref); ok {
		name = n
	} else {
		name = strings.TrimSuffix(strings.TrimPrefix(ref, projectCategoryPrefix+"/"), ".md")
	}
	// A fallback name with a path separator is never a project: ProjectNameOf
	// already rejected the ref (reserved bucket like 프로젝트/거래/…, or another
	// category), and letting "거래/탑솔라" through would make the legacy lookup
	// below treat the 거래 ledger page as a rep page and archive it.
	if name == "" || strings.Contains(name, "/") || isReservedProjectDir(name) {
		return "", "", fmt.Errorf("wiki: %q is not a project", ref)
	}
	if _, rerr := s.ReadPage(RepPagePath(name)); rerr == nil {
		return name, RepPagePath(name), nil
	}
	legacy := projectCategoryPrefix + "/" + name + ".md"
	if _, rerr := s.ReadPage(legacy); rerr == nil {
		return name, legacy, nil
	}
	// Display-title fallback: a project whose page Title differs from its folder
	// name ("기아 화성" vs 기아-화성) is addressable by the name the user actually
	// says. KnownProjects lists ACTIVE projects only, so this serves close (and
	// text-form reopen of a closed project still needs the folder name or path).
	for _, kp := range s.knownProjects() {
		if !strings.EqualFold(strings.TrimSpace(kp.Name), name) {
			continue
		}
		if n, ok := ProjectNameOf(kp.Path); ok {
			return n, kp.Path, nil
		}
	}
	return "", "", fmt.Errorf("wiki: project %q not found (대표페이지 없음)", name)
}

// projectFolderPages lists every page belonging to the project (rep, 로그,
// 기자재, 메일분석, details) — ProjectFolderOf matches the legacy flat rep page
// AND any in-folder children, so a transitional project (flat rep + folder
// children) returns all of them. The repPath fallback covers only the case
// where the listing itself came back empty.
func (s *Store) projectFolderPages(name, repPath string) []string {
	pages, err := s.ListPages(projectCategoryPrefix)
	if err != nil {
		return []string{repPath}
	}
	folder := projectCategoryPrefix + "/" + name
	var out []string
	for _, p := range pages {
		p = strings.ReplaceAll(p, "\\", "/")
		if f, ok := ProjectFolderOf(p); ok && f == folder {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		out = []string{repPath}
	}
	return out
}

// CloseProject retires a project: records the closure (date + outcome note) in
// the 대표페이지's "## 종결" section and archives every page in the project
// folder. Idempotent-ish: closing an already-closed project refreshes the
// record and re-archives stragglers. No files move or disappear.
func (s *Store) CloseProject(ref, note string, now time.Time) (closeResult, error) {
	name, repPath, err := s.resolveProjectRep(ref)
	if err != nil {
		return closeResult{}, err
	}
	date := now.Format("2006-01-02")

	// 1. Closure record on the 대표페이지 (kept even while archived — this is the
	//    answer to "그 건 어떻게 끝났지?").
	repWasArchived := false
	if err := s.UpdatePage(repPath, func(cur *Page) (*Page, error) {
		if cur == nil {
			return nil, fmt.Errorf("대표페이지 없음: %s", repPath)
		}
		repWasArchived = cur.Meta.Archived
		content := "- 종결일: " + date
		if n := strings.TrimSpace(note); n != "" {
			content += "\n- 결과: " + n
		}
		if prev := strings.TrimSpace(cur.Section(closedSectionHeading)); prev != "" {
			content = prev + "\n" + content // keep prior close/reopen history
		}
		cur.Body = upsertSection(cur.Body, closedSectionHeading, content)
		cur.Meta.Archived = true
		cur.Meta.Updated = date
		return cur, nil
	}); err != nil {
		return closeResult{}, fmt.Errorf("wiki: close %s: %w", name, err)
	}

	// 2. Archive the whole folder. Archived counts pages whose flag actually
	//    flipped — re-closing an already-closed project refreshes the record but
	//    reports 0 newly archived, not a phantom rep.
	archived := 0
	if !repWasArchived {
		archived = 1
	}
	for _, p := range s.projectFolderPages(name, repPath) {
		if p == repPath {
			continue
		}
		changed := false
		if err := s.UpdatePage(p, func(cur *Page) (*Page, error) {
			if cur == nil || cur.Meta.Archived {
				return nil, nil
			}
			cur.Meta.Archived = true
			cur.Meta.Updated = date
			changed = true
			return cur, nil
		}); err == nil && changed {
			archived++
		}
	}
	_ = s.appendLog("close-project", repPath+" — "+strings.TrimSpace(note))
	return closeResult{RepPath: repPath, Archived: archived}, nil
}

// ReopenProject reverses CloseProject: un-archives every page in the project
// folder and appends a 재개 line to the 종결 record (history stays visible).
func (s *Store) ReopenProject(ref string, now time.Time) (reopenResult, error) {
	name, repPath, err := s.resolveProjectRep(ref)
	if err != nil {
		return reopenResult{}, err
	}
	date := now.Format("2006-01-02")

	logArchive := LogArchivePath(name)
	restored := 0
	for _, p := range s.projectFolderPages(name, repPath) {
		// The rotated-log archive (로그-보관.md) is archived by RotateProjectLog,
		// not by closure — leave it archived (search-demoted) after reopen so the
		// old rotated-out log sections don't resurface.
		if p == logArchive {
			continue
		}
		changed := false
		if err := s.UpdatePage(p, func(cur *Page) (*Page, error) {
			if cur == nil || !cur.Meta.Archived {
				// Already active (rep included): no restore, no spurious 재개
				// history line, no Updated churn. Reopening a fully-active
				// project must fall through to the "이미 활성" error below.
				return nil, nil
			}
			cur.Meta.Archived = false
			cur.Meta.Updated = date
			if p == repPath {
				if prev := strings.TrimSpace(cur.Section(closedSectionHeading)); prev != "" {
					cur.Body = upsertSection(cur.Body, closedSectionHeading, prev+"\n- "+date+": 재개")
				}
			}
			changed = true
			return cur, nil
		}); err == nil && changed {
			restored++
		}
	}
	if restored == 0 {
		return reopenResult{RepPath: repPath}, fmt.Errorf("wiki: reopen %s: 보관된 페이지 없음 (이미 활성)", name)
	}
	_ = s.appendLog("reopen-project", repPath)
	return reopenResult{RepPath: repPath, Restored: restored}, nil
}

// FlagDormantProjects proposes closure for ACTIVE projects with no page
// activity for dormantAfterDays: one dated bullet on the 대표페이지's 현재 상태
// (surfaces in the 모아보기), idempotent per quarter via the provenance ref,
// capped per call. Never closes anything itself. Returns flagged rep paths.
//
// Activity basis: the newest Updated date across the project's pages, read
// from the master index (no page I/O). The flag bullet itself stamps Updated,
// so a flagged project naturally leaves the dormant set until next quarter.
func (s *Store) FlagDormantProjects(now time.Time, maxFlags int) []string {
	if maxFlags <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -dormantAfterDays).Format("2006-01-02")
	quarter := fmt.Sprintf("dormant:%dQ%d", now.Year(), (int(now.Month())-1)/3+1)

	// Snapshot — never walk the live index map (writers mutate it in place).
	lastByFolder := make(map[string]string) // 프로젝트/<name> → newest Updated
	for path, entry := range s.snapshotEntries() {
		folder, ok := ProjectFolderOf(path)
		if !ok {
			continue
		}
		if entry.Updated > lastByFolder[folder] {
			lastByFolder[folder] = entry.Updated
		}
	}

	var flagged []string
	for _, ref := range s.knownProjects() { // active projects only
		name, ok := ProjectNameOf(ref.Path)
		if !ok {
			continue
		}
		last := lastByFolder[projectCategoryPrefix+"/"+name]
		if last == "" || last >= cutoff { // ISO dates compare lexicographically
			continue
		}
		// Already flagged this quarter (the provenance marker embeds the ref):
		// skip so the per-call cap goes to projects that still need the nudge.
		if page, rerr := s.ReadPage(ref.Path); rerr == nil && page != nil &&
			strings.Contains(page.Body, quarter) {
			continue
		}
		lastDate := mustDate(last)
		if lastDate.IsZero() {
			continue // unparseable Updated stamp — don't emit a nonsense month count
		}
		months := int(now.Sub(lastDate).Hours() / 24 / 30)
		line := fmt.Sprintf("약 %d개월간 활동 없음 — 종결 검토 제안 (\"%s 종결 처리\"라고 말하면 정리됩니다)", months, ref.Name)
		if err := s.AppendProjectStatusLine(ref.Path, line, quarter, now); err != nil {
			continue
		}
		flagged = append(flagged, ref.Path)
		if len(flagged) >= maxFlags {
			break
		}
	}
	return flagged
}

// mustDate parses a YYYY-MM-DD date, zero time on failure.
func mustDate(d string) time.Time {
	t, err := time.Parse("2006-01-02", d)
	if err != nil {
		return time.Time{}
	}
	return t
}
