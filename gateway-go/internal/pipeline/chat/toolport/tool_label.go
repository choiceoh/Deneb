// tool_label.go — gateway-owned Korean names for the tools a turn calls.
//
// Sibling of ChatProgressLabel, for the same reason: one wording source for
// every surface that shows tool activity. The phone carried a curated table of
// its own (client-android ToolStatusLabels) while Andromeda rendered the raw
// identifier — so a Korean-first product showed "gmail" and "phone_read" on the
// desktop and "메일 확인" on the phone for the same call. Vocabulary belongs to
// the gateway; clients render what they are given.
//
// The NOUN form is canonical ("메일 확인"). Tense is the surface's business: a
// running chip says "…중", a finished one "…완료", a trail entry the bare noun.
// Sending the progressive form instead would force every client to strip it.
package toolport

// chatToolLabels is the curated set. A tool absent here is deliberate, not an
// oversight — see ChatToolLabel for what happens to it.
var chatToolLabels = map[string]string{
	"calendar":         "일정 확인",
	"clarify":          "질문 정리",
	"people":           "사람 조회",
	"cron":             "예약 작업 처리",
	"edit":             "파일 수정",
	"exec":             "명령 실행",
	"fetch_tools":      "도구 준비",
	"gateway":          "게이트웨이 점검",
	"gmail":            "메일 확인",
	"graphify":         "지식 그래프 작업",
	"grep":             "자료 검색",
	"heartbeat_update": "상태 메모 갱신",
	"knowledge":        "지식 검색",
	"message":          "메시지 전송",
	"morning_letter":   "아침 편지 작성",
	"observe":          "시스템 점검",
	"phone_read":       "휴대폰 확인",
	"phone_write":      "휴대폰 제어",
	"polaris":          "컨텍스트 정리",
	"process":          "작업 프로세스 확인",
	"read":             "파일 확인",
	"read_spillover":   "추가 출력 확인",
	"send_file":        "파일 전송",
	"sessions":         "세션 확인",
	"sessions_spawn":   "보조 세션 시작",
	"skills":           "스킬 확인",
	"watch":            "감시 작업 설정",
	"web":              "웹 검색",
	"wiki":             "기억 검색",
	"write":            "파일 작성",
}

// ChatToolLabel returns the Korean name for a tool, or "" when there is none.
//
// Empty rather than a guessed label: a client that gets "" falls back to its own
// rendering of the identifier, which is honest about being unnamed. Inventing a
// label from the raw name (say, title-casing it) would look curated while being
// nothing of the sort, and would hide that the tool needs an entry here.
func ChatToolLabel(tool string) string { return chatToolLabels[tool] }

// LabelledToolNames returns every tool name the label map covers. Exported for
// the toolwire contract test that catches a label left behind by a removed tool
// (or a live tool with no label) — the map is otherwise unreachable from there.
func LabelledToolNames() []string {
	out := make([]string, 0, len(chatToolLabels))
	for name := range chatToolLabels {
		out = append(out, name)
	}
	return out
}
