// project_log.go — bounded growth for the per-project 로그.md slot.
//
// The layout routes every event/meeting/approval onto the project's 로그.md
// (instead of minting new pages), which means an active project's log grows
// without bound. RotateProjectLog keeps the log page a readable working set:
// sections beyond the newest LogKeepSections move to the project's 로그-보관.md
// (archived, so search demotes it and Tier-1/research skip it) — lossless
// rotation, no LLM. Called by the wiki-review background task.
package wiki

import (
	"fmt"
	"strings"
	"time"
)

// LogKeepSections is how many newest H2 sections stay in a project's 로그.md.
const LogKeepSections = 20

// logArchiveFile is the per-project rotated-log page filename.
const logArchiveFile = "로그-보관.md"

// LogArchivePath returns the rotated-log page path for a project.
func LogArchivePath(project string) string {
	return projectCategoryPrefix + "/" + project + "/" + logArchiveFile
}

// IsProjectLogPage reports whether relPath is a project's 로그.md or 로그-보관.md
// slot. Log slots are path-fixed by construction, so duplicate review of them
// is meaningless (two projects' logs legitimately share titles/shape).
func IsProjectLogPage(relPath string) bool {
	seg := splitProjectPath(relPath)
	if len(seg) != 2 || isReservedProjectDir(seg[0]) {
		return false
	}
	return seg[1] == LogPageFile || seg[1] == logArchiveFile
}

// RotateProjectLog moves a project 로그.md's oldest sections (beyond the newest
// LogKeepSections) into its 로그-보관.md. Sections are appended chronologically,
// so "oldest" = the head of the section list. Crash-safe ordering: the archive
// gains the sections before the log drops them (a crash between leaves a
// harmless duplicate, recoverable via git). Returns sections moved.
func (s *Store) RotateProjectLog(project string) (int, error) {
	logPath := LogPagePath(project)
	logPage, err := s.ReadPage(logPath)
	if err != nil || logPage == nil {
		//nolint:nilerr // best-effort rotation: an unreadable log is treated as absent
		return 0, nil // no log yet — nothing to rotate
	}
	_, sections := logPage.SplitByH2()
	if len(sections) <= LogKeepSections {
		return 0, nil
	}
	overflow := sections[:len(sections)-LogKeepSections]

	// 1. Append the overflow to the archive page (created on first rotation).
	archivePath := LogArchivePath(project)
	if err := s.UpdatePage(archivePath, func(cur *Page) (*Page, error) {
		if cur == nil {
			cur = NewPage(project+" 로그 보관", projectCategoryPrefix, nil)
			cur.Meta.Type = "log"
			cur.Meta.Archived = true
			cur.Meta.Summary = project + " 진행 로그의 회전 보관분 (로그.md에서 이월)"
			cur.Meta.Importance = 0.2
			cur.Body = "# " + project + " 로그 보관\n"
		}
		var b strings.Builder
		b.WriteString(strings.TrimRight(cur.Body, "\n"))
		for _, sec := range overflow {
			b.WriteString("\n\n## ")
			b.WriteString(sec.Heading)
			if c := strings.TrimSpace(sec.Content); c != "" {
				b.WriteString("\n\n")
				b.WriteString(c)
			}
		}
		cur.Body = b.String()
		cur.Meta.Updated = time.Now().Format("2006-01-02")
		return cur, nil
	}); err != nil {
		return 0, fmt.Errorf("wiki: rotate log archive %q: %w", archivePath, err)
	}

	// 2. Trim the log page down to the kept tail. The trim re-splits CUR (read
	//    under the write lock), not the pre-read snapshot: a section appended
	//    between the read above and this mutate would be silently rebuilt away
	//    by the stale snapshot. We drop exactly the archived head sections
	//    (verified by heading) and keep everything after — concurrent appends
	//    land at the tail and survive.
	if err := s.UpdatePage(logPath, func(cur *Page) (*Page, error) {
		return trimRotatedLogSections(cur, overflow), nil
	}); err != nil {
		return 0, fmt.Errorf("wiki: rotate log trim %q: %w", logPath, err)
	}
	_ = s.appendLog("rotate-log", fmt.Sprintf("%s — %d개 섹션 → %s", logPath, len(overflow), archivePath))
	return len(overflow), nil
}

// trimRotatedLogSections rebuilds a log page's body from its CURRENT state
// minus the already-archived head sections. It drops exactly len(overflow)
// head sections after verifying their headings still match what was archived;
// on any mismatch (log deleted, rewritten, or shrunk concurrently) it returns
// nil — skip the write, the next rotation cycle re-runs from fresh state (the
// archive tolerating a duplicated section is the documented crash-safe
// tradeoff). Sections appended after the pre-read land at the tail and are
// preserved.
func trimRotatedLogSections(cur *Page, overflow []H2Section) *Page {
	if cur == nil || len(overflow) == 0 {
		return nil
	}
	preamble, sections := cur.SplitByH2()
	if len(sections) < len(overflow) {
		return nil
	}
	for i, sec := range overflow {
		if sections[i].Heading != sec.Heading {
			return nil
		}
	}
	var b strings.Builder
	if p := strings.TrimSpace(preamble); p != "" {
		b.WriteString(p)
	}
	for _, sec := range sections[len(overflow):] {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## ")
		b.WriteString(sec.Heading)
		if c := strings.TrimSpace(sec.Content); c != "" {
			b.WriteString("\n\n")
			b.WriteString(c)
		}
	}
	cur.Body = b.String()
	cur.Meta.Updated = time.Now().Format("2006-01-02")
	return cur
}
