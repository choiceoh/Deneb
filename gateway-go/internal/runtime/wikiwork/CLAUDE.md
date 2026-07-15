# Wiki background work tasks map

Owns scheduled/background wiki maintenance tasks: scout, research, review,
site-visit recording, meeting attendance, and digest tasks. Composition
roots construct tasks with a `*wiki.Store`.

## Entry points

- `wiki_scout_task.go` — `NewScoutTask`, `ScoutTask`
- `wiki_research_task.go` — `NewResearchTask`, `ResearchTask`
- `wiki_review_task.go` — `NewReviewTask`, `ReviewTask`
- `site_visit.go` — `NewSiteVisitRecorder`, `SiteVisitRecorder`
- `meeting_attendance.go` — `RecordMeetingAttendanceByPath`
- `noti_digest_task.go` / `supernote_digest_task.go` — digest task ctors

## Dependency direction and invariants

- **Dependency / boundary**: tasks depend on `domain/wiki` Store APIs and
  must not import chat turn execution or RPC handlers. Server/cron wires
  the Store and schedules `Run`.
- **Invariant**: wiki writes must stay project-scoped and idempotent where
  tasks re-run; attendance/site-visit recorders must never create pages
  outside the intended project path; boundary matrix tests encode the
  forbid list for cross-cutting imports.
- Prefer narrow Store helpers over reaching into dreamer internals.

## Local change scope

Keep background wiki jobs inside `wikiwork/`.

- May co-change: `domain/wiki` helpers the tasks call, and cron/server
  registration of task ctors.
- Do not touch: chat recall preflight or genesis evolution loops.

## Focused verification

```
cd gateway-go && go test -count=1 ./internal/runtime/wikiwork
```
