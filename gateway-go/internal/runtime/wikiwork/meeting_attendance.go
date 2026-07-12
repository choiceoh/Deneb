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

// RecordMeetingAttendanceByPath appends a silent 회의 op to the 로그.md of the
// project owning repPath (a 대표페이지 path already resolved from a TYPED project
// match — never a bare name re-interpreted here, so a counterparty whose name
// collides with a project folder can't be mislogged). dateISO is the meeting's
// date (YYYY-MM-DD). Returns true when the event is HANDLED — a line was
// written, OR repPath resolves to no project (nothing to write). Returns false
// ONLY on a transient wiki write failure, so the caller retries next poll.
func RecordMeetingAttendanceByPath(store *wiki.Store, repPath, title, dateISO string) bool {
	if store == nil || dateISO == "" {
		return true // nothing to write and nothing to retry
	}
	folder, ok := wiki.ProjectNameOf(strings.TrimSpace(repPath))
	if !ok || folder == "" {
		return true // not a project path — deliberate skip
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "회의"
	}
	section := "## [" + dateISO + "] 회의 | " + title + " — 참석 (캘린더 기반)\n"
	werr := store.UpdatePage(wiki.LogPagePath(folder), func(cur *wiki.Page) (*wiki.Page, error) {
		if cur == nil {
			p := wiki.NewPage(folder+" 진행 로그", "프로젝트", nil)
			p.Meta.Type = "log"
			p.Meta.Summary = folder + " 진행 로그"
			p.Body = section
			return p, nil
		}
		cur.Body = strings.TrimRight(cur.Body, "\n") + "\n\n" + section
		cur.Meta.Updated = dateISO
		return cur, nil
	})
	return werr == nil
}
