package chrono

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/routine"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/schedule"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
)

// Register wires schedule/routine tools owned by RegisterCoreTools.
func Register(registry toolport.ToolRegistrar, deps *tooldeps.CoreToolDeps) {
	var diaryDir, wikiDir string
	if deps.Wiki.Store != nil {
		diaryDir = deps.Wiki.Store.DiaryDir()
		wikiDir = deps.Wiki.Store.Dir()
	}
	var filesSemantic artifact.FilesSemanticSearchFunc
	if deps.FilesSemanticSearch != nil {
		filesSemantic = func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error) {
			hits, err := deps.FilesSemanticSearch(ctx, query, max)
			if err != nil {
				return nil, err
			}
			out := make([]filestore.ScoredEntry, len(hits))
			for i, h := range hits {
				out[i] = filestore.ScoredEntry{
					Entry:   filestore.Entry{PathDisplay: h.Path, Name: h.Name, ID: h.Path},
					Score:   h.Score,
					Snippet: h.Snippet,
				}
			}
			return out, nil
		}
	}
	RegisterRoutineTools(registry, &deps.Chrono, diaryDir, wikiDir, filesSemantic)
	RegisterTodoTool(registry)
}

// RegisterRoutineTools registers tools for recurring/scheduled tasks —
// things that sit between always-on core tools and on-demand skills.
// Typical trigger: cron scheduler, daily routines, periodic checks.
// diaryDir is the wiki diary directory for morning letter logging; wikiDir is
// the wiki root for its deadline scan (either empty = that part disabled).
func RegisterRoutineTools(registry toolport.ToolRegistrar, chrono *tooldeps.ChronoDeps, diaryDir, wikiDir string, filesSemanticSearch artifact.FilesSemanticSearchFunc) {
	// Deferred (prompt audit 2026-06-12): ~590 wire tokens — the second-largest
	// eager tool — for 11 interactive uses in 14 days. The scheduler itself runs
	// server-side; this tool only manages jobs, so a "매일 아침에 …" turn pays one
	// fetch round-trip instead of every turn paying the schema. No cron job
	// prompt directs the cron tool by name (the static Tool Usage trigger line
	// "for follow-ups use cron" stays, pointing at the deferred listing).
	registry.RegisterTool(toolport.ToolDef{
		Name:        "cron",
		Description: "Schedule recurring jobs (cron expressions). Actions: status, list, add, update, remove, run, get, runs, wake",
		InputSchema: schema.CronToolSchema(),
		Fn:          schedule.ToolCron(chrono),
		Deferred:    true,
	})

	registry.RegisterTool(toolport.ToolDef{
		Name:        "files",
		Description: "파일 저장소 (로컬 디스크, 외부 클라우드 아님): list, search (이름·content=true로 내용, semantic=true로 의미 기반 벡터 검색), semantic_search (=search semantic=true), download (extract=true로 텍스트 추출 — PDF/이미지 OCR·Excel/Word/PowerPoint), upload (로컬 파일을 저장소에 저장), share (7일 유효 공유 링크), analyze (문서 내용 추출). 저장 위치: DENEB_FILES_DIR (기본 ~/.deneb/files). 인증 불필요.",
		InputSchema: schema.FilesToolSchema(),
		Fn:          artifact.ToolFiles(filesSemanticSearch),
		Deferred:    true,
	})
	// Morning-letter collection + deterministic card assembly. The raw sections
	// remain in the result for inspection, but delivery is complete and should be
	// returned verbatim. Deferred like the other routine tools.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "morning_letter",
		Description: "모닝레터 완성: 날씨·달러환율·구리·일정·메일·위키 마감/미해결질문·전자결재를 병렬 수집하고 delivery에 검증 가능한 deneb-ui 카드를 완성해 반환한다. delivery를 그대로 최종 응답으로 사용하고 sections는 다시 조회·재구성하지 않는다. No parameters",
		InputSchema: schema.MorningLetterToolSchema(),
		Fn:          routine.ToolMorningLetter(routine.MorningLetterOpts{DiaryDir: diaryDir, WikiDir: wikiDir}),
		Deferred:    true,
	})
	// Evening-letter data collection: the end-of-day counterpart to
	// morning_letter — forward-looking sections (calendar, email, deadlines),
	// the morning-only market data omitted. Deferred like the other routine tools.
	// The output contract below used to live only in the retired evening-letter
	// SKILL.md (#3059) — it rides the description now (full text arrives at
	// fetch_tools time) so manual invocations keep the native card format.
	registry.RegisterTool(toolport.ToolDef{
		Name: "evening_letter",
		Description: "이브닝레터 데이터 수집: 일정(오늘+내일)·미처리 메일·임박 마감을 병렬 수집해 raw JSON 반환. " +
			"모닝레터의 저녁 짝 — 시장데이터(날씨·환율·구리)는 제외. 편지 작성(회고·내일 준비·우선순위)은 에이전트 몫. " +
			"출력 계약: 차분한 머리말 한 줄(펜스 밖) + deneb-ui 펜스 블록 정확히 1개(루트 <column> 하나의 라벨 HTML 마크업, " +
			"태그는 column/card/row/text(style: headline·caption·body)/ul·li/icon/badge/hr만 — 예: <card><row><icon name=\"calendar\" size=\"16\"/>" +
			"<text style=\"caption\">내일 일정</text></row><ul><li>10:00 — 분기 리뷰</li></ul></card>). " +
			"맨 앞에 마스트헤드: <text style=\"headline\">M월 D일 요일 저녁</text> + <text style=\"caption\">이브닝 레터 · 데네브</text> + <hr/>. " +
			"이어서 카드 3장 — 내일 일정(icon calendar)·" +
			"챙길 메일(icon mail, 상위 3~5건)·임박 마감(icon alarm, 항목마다 <badge>D-N</badge> — 0=\"D-day\", 음수=\"기한 초과\", " +
			"긴급도 색: D-3 이하 color=\"warning\", 기한 초과·D-day color=\"error\", 그 외 색 없음; 빈 섹션은 카드째 생략, " +
			"ok:false 섹션은 본문에 '조회 실패'). 최종 텍스트가 곧 전달 메시지 — message 툴 호출·확인 문구·채널 상태 추측 금지. " +
			"No parameters",
		InputSchema: schema.EveningLetterToolSchema(),
		Fn:          routine.ToolEveningLetter(routine.EveningLetterOpts{DiaryDir: diaryDir, WikiDir: wikiDir}),
		Deferred:    true,
	})
}

// RegisterCalendarTool registers the calendar tool: read merged Google (read-only)
// + local events, and create/update/delete local events. Skipped when neither a
// Google client factory nor a local store is wired, so the agent doesn't see a
// dead surface. This is the chat-side twin of the miniapp.calendar.* RPC surface.
func RegisterCalendarTool(registry toolport.ToolRegistrar, calDeps *tooldeps.CalendarDeps) {
	if calDeps.Client == nil && calDeps.Local == nil {
		return
	}
	registry.RegisterTool(toolport.ToolDef{
		Name: "calendar",
		Description: "캘린더 일정 조회·관리. list(다가오는 일정), get(상세 — 참석자·장소·Meet·메모, 미팅 준비용), create(추가), update(수정), delete(삭제). " +
			"구글 캘린더(읽기)와 로컬 일정(읽기·쓰기)을 합쳐 보여주며 추가·수정·삭제는 로컬 일정에만 적용된다. " +
			"사용자가 '오늘/이번 주 일정', '내일 3시 미팅 잡아줘', 'OOO 일정 언제야', '미팅 준비' 같이 일정을 묻거나 시키면 짐작하지 말고 호출하라.",
		InputSchema: schema.CalendarToolSchema(),
		Fn:          schedule.ToolCalendar(calDeps),
	})
}

// RegisterTodoTool registers the user 할일 tool (localtodo store).
func RegisterTodoTool(registry toolport.ToolRegistrar) {
	// Deferred (2026-07-09): the native client owns the user's 할일 list
	// (miniapp.todo.*) as the primary surface, and no prompt trigger names this
	// tool — chat-side todo edits are occasional, so an "할일 추가/목록" turn fetches it.
	registry.RegisterTool(toolport.ToolDef{
		Name: "todo",
		Description: "Manage the user's 할일 (to-do) list — the SAME localtodo store the native client reads via miniapp.todo.*. " +
			"Actions: list | add (needs title; optional due YYYY-MM-DD) | done (needs id; optional done=false to un-complete) | delete (needs id). " +
			"Use THIS for the user's checkable tasks (a to-do added here appears on the user's device); heartbeat_update is the agent's own free-form work memo, not the user's task list.",
		InputSchema: schema.TodoToolSchema(),
		Fn:          schedule.ToolTodo(),
		Deferred:    true,
	})
}
