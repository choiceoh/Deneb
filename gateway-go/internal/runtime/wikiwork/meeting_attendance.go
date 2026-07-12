// meeting_attendance.go — silently record that the operator attended a
// work-linked meeting.
//
// The meeting-harvest asks a follow-up ("○○ 어떻게 되셨어요?") whose reply the
// dreamer files into the project log — but only when the operator replies. If
// they don't, the meeting fact evaporated. This records attendance
// deterministically the moment a matched meeting ends, so "기아PE와 미팅함
// (2026-07-13)" is remembered regardless of the reply. The outcome (from a
// reply) still layers on top via the normal flow.
//
// Attribution is project-only: a counterparty-only match has no single project
// log to write to, so it is skipped (the ask still fires for it).
package wikiwork

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// RecordMeetingAttendance appends a silent 회의 op to a project's 로그.md when
// target resolves to a known project. dateISO is the meeting's date
// (YYYY-MM-DD). Returns true when a line was written. store nil, empty target,
// or a counterparty-only target ⇒ false (no-op).
func RecordMeetingAttendance(store *wiki.Store, target, title, dateISO string) bool {
	if store == nil {
		return false
	}
	target = strings.TrimSpace(target)
	if target == "" || dateISO == "" {
		return false
	}
	// Resolve to a project rep (folder rep, legacy flat, or display title). A
	// counterparty-only match errors here and is skipped by design.
	project, _, err := store.ResolveProjectRep(target)
	if err != nil || project == "" {
		return false
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "회의"
	}
	section := "## [" + dateISO + "] 회의 | " + title + " — 참석 (캘린더 기반)\n"
	werr := store.UpdatePage(wiki.LogPagePath(project), func(cur *wiki.Page) (*wiki.Page, error) {
		if cur == nil {
			p := wiki.NewPage(project+" 진행 로그", "프로젝트", nil)
			p.Meta.Type = "log"
			p.Meta.Summary = project + " 진행 로그"
			p.Body = section
			return p, nil
		}
		cur.Body = strings.TrimRight(cur.Body, "\n") + "\n\n" + section
		cur.Meta.Updated = dateISO
		return cur, nil
	})
	return werr == nil
}
