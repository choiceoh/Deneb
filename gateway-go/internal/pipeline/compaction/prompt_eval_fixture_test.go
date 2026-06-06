package compaction

// Prompt evaluation fixture: a realistic Korean "비서실장형" work conversation
// (업무분석 메일/프로젝트/사람/딜 + 업무비서 일정/미팅/캡처) plus the gold facts
// that any faithful compaction summary MUST preserve. The fixture is the shared
// ground truth used by both the deterministic scorer test and the live
// model-backed A/B harness (prompt_eval_test.go).

import (
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// goldFact is a single piece of information the source conversation contains
// that a good summary must carry forward. A fact counts as "preserved" when at
// least one of its surface forms (any) appears in the summary — this tolerates
// formatting differences (2.4억 vs 2억 4천만) without rewarding hallucination.
type goldFact struct {
	id   string   // short stable id for the comparison table
	desc string   // human description (analyst note)
	any  []string // preserved if ANY of these substrings appears in the summary
}

// goldFacts is the checklist. These are the facts a 비서실장 must not lose to
// resume the deal/calendar context after a compaction.
func goldFacts() []goldFact {
	return []goldFact{
		{"거래처", "거래 상대 회사명", []string{"탑솔라", "Topsolar"}},
		{"담당자", "상대측 의사결정자", []string{"김민준"}},
		{"계약금액", "계약 총액 2.4억", []string{"2.4억", "2억 4", "2억4", "240,000,000", "2.4 억"}},
		{"가격인하", "5% 인하 합의", []string{"5%", "5 %", "5퍼", "5 퍼"}},
		{"미팅일", "6월 14일 대면 미팅", []string{"6월 14일", "6/14", "6월14일", "06-14", "14일"}},
		{"NDA마감", "6월 20일 NDA 회신 마감", []string{"6월 20일", "6/20", "6월20일", "20일"}},
		{"열린작업", "NDA 초안 회신 대기", []string{"NDA"}},
		{"경쟁사", "경쟁사 한빛에너지 리스크", []string{"한빛에너지", "한빛"}},
		{"결제조건", "3회 분할 납부", []string{"3회 분할", "3회", "분할"}},
		{"메일회신", "탑솔라 측 회신 수신 사실", []string{"회신", "답장", "reply", "회답"}},
		{"녹음캡처", "6/7 통화 녹음 전사 캡처", []string{"녹음", "전사", "통화"}},
	}
}

// forbiddenLeakTokens are internal markers that must never appear in a delivered
// summary. Their presence is a quality penalty (the model leaked machinery).
var forbiddenLeakTokens = []string{
	"<thinking", "NO_REPLY", "SILENT_REPLY", "<function", "<tool_call", "cache_control",
}

// fixtureMessages builds the source transcript. It deliberately mixes:
//   - user asks (analyst questions about a deal)
//   - assistant gmail tool_use + tool_result (a real-ish mail thread)
//   - people/relationship context (분석 모드)
//   - calendar + deadline + capture (비서 모드)
//   - a correction (price changed) to exercise the "record only the updated value" rule
func fixtureMessages() []llm.Message {
	toolUse := func(id, name string, input map[string]any) llm.ContentBlock {
		raw, _ := json.Marshal(input)
		return llm.ContentBlock{Type: "tool_use", ID: id, Name: name, Input: raw}
	}
	toolResult := func(id, content string) llm.Message {
		return llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: id, Content: content},
		})
	}
	asstText := func(s string) llm.Message { return llm.NewTextMessage("assistant", s) }
	asstTool := func(s, id, name string, input map[string]any) llm.Message {
		return llm.NewBlockMessage("assistant", []llm.ContentBlock{
			{Type: "text", Text: s}, toolUse(id, name, input),
		})
	}
	userText := func(s string) llm.Message { return llm.NewTextMessage("user", s) }

	return []llm.Message{
		userText("탑솔라 딜 어디까지 진행됐는지 정리해줘. 메일 확인하고."),
		asstTool("탑솔라 거래 관련 최근 메일을 확인하겠습니다.", "t1", "gmail",
			map[string]any{"action": "search", "query": "from:탑솔라 OR 계약"}),
		toolResult("t1", "메일 3건 발견:\n"+
			"1) [탑솔라] 김민준 부장 <minjun.kim@topsolar.co.kr> — '태양광 모듈 공급계약 회신' (6/5 수신)\n"+
			"   본문: 제안 주신 계약 총액 2.4억(VAT 별도)은 내부 검토 통과했습니다. 다만 단가 5% 인하가 가능한지 회신 바랍니다. "+
			"결제는 3회 분할(계약 시 40%, 납품 시 40%, 검수 후 20%)을 희망합니다.\n"+
			"2) [내부] 영업팀 — '경쟁사 동향' (6/4): 한빛에너지가 탑솔라에 유사 견적(2.6억)을 제출했다는 첩보. 가격 경쟁 주의.\n"+
			"3) [탑솔라] 김민준 부장 — '대면 미팅 제안' (6/5): 6월 14일(금) 오후 2시 본사에서 최종 협의 희망. NDA 초안은 6월 20일까지 회신 요청."),
		asstText("메일 확인했습니다. 탑솔라 김민준 부장이 계약 총액 2.4억(VAT 별도)은 승인했고, 단가 5% 인하를 요청했습니다. " +
			"경쟁사 한빛에너지가 2.6억 견적을 넣어 가격 압박이 있습니다. 6월 14일 대면 미팅, NDA 초안은 6월 20일 마감입니다."),
		userText("5% 인하는 마진 보면 어려워. 대신 3% 인하하고 결제조건을 우리한테 유리하게 틀자. 그리고 14일 미팅 일정 잡아줘."),
		asstTool("3% 인하 방향으로 메모하고, 6월 14일 미팅을 캘린더에 등록하겠습니다.", "t2", "calendar",
			map[string]any{"action": "create", "title": "탑솔라 최종 협의 (김민준 부장)", "date": "2026-06-14", "time": "14:00"}),
		toolResult("t2", "일정 생성됨: '탑솔라 최종 협의 (김민준 부장)' 2026-06-14 14:00, 장소 탑솔라 본사. 리마인더 1일 전 설정."),
		asstText("6월 14일 14시 미팅 등록했습니다. 협상 카드는 '단가 3% 인하 + 결제조건 변경(계약 시 50% 선납)'으로 잡았습니다. " +
			"단가 인하폭은 5%가 아니라 3%로 확정 제안하는 것이 마진상 안전합니다."),
		userText("좋아. 어제 김부장이랑 통화한 녹음 있는데 그것도 참고해. 그리고 NDA 초안 누가 보내기로 했지?"),
		asstTool("통화 녹음을 전사해 참고하겠습니다.", "t3", "asr",
			map[string]any{"action": "transcribe", "file": "topsolar_call_0606.m4a"}),
		toolResult("t3", "전사 결과 (6/6 통화, 화자분리):\n"+
			"[00:12 김민준] 단가는 솔직히 5%까지는 바라지도 않아요. 3% 정도면 윗선 설득 가능합니다.\n"+
			"[01:40 김민준] NDA 초안은 그쪽(우리)에서 먼저 보내주시는 걸로 하죠. 6월 20일 전까지만 오면 됩니다.\n"+
			"[02:05 사용자] 알겠습니다. 분할 결제는 조정 좀 할게요."),
		asstText("녹음 확인 결과: 김민준 부장은 3% 인하면 내부 설득이 된다고 했습니다(우리 안과 일치). " +
			"NDA 초안은 **우리 쪽에서 먼저** 작성해 6월 20일 전까지 보내기로 했습니다. 이 작업이 아직 미완료 상태입니다."),
		userText("오케이. NDA 초안 작성은 내가 법무팀에 돌릴게. 너는 14일 미팅 전까지 협상 브리핑 한 장 준비해둬."),
		asstText("알겠습니다. NDA 초안은 법무팀 진행으로 기록하고, 6월 14일 미팅용 협상 브리핑(단가 3% + 결제 선납 50%, 경쟁사 한빛 대비 우위 포인트)을 준비해두겠습니다."),
	}
}

// fixtureText is the serialized form fed to the summarizer (same path the real
// pipeline uses via serializeMessages).
func fixtureText() string {
	return serializeMessages(fixtureMessages())
}

// fixtureSummaryForFacts is a hand-written reference summary that preserves
// every gold fact — used by the deterministic-scorer unit test as the "good"
// case (no model required). It intentionally uses varied surface forms.
func fixtureSummaryForFacts() string {
	return "### 핵심 사실 (Facts)\n" +
		"- [확실] 거래처: 탑솔라(Topsolar), 담당 김민준 부장\n" +
		"- [확실] 계약 총액 2.4억(VAT 별도), 결제 3회 분할 협의 중\n" +
		"- [확실] 단가 인하: 상대 5% 요청 → 우리 3%로 확정 제안(녹음상 김부장도 3% 수용 시사)\n" +
		"- [확실] 경쟁사 한빛에너지 2.6억 견적 제출(가격 압박)\n" +
		"### 열린 루프 (Open Loops)\n" +
		"- [진행중] NDA 초안: 우리(법무팀)가 작성해 6월 20일까지 회신\n" +
		"- [대기] 6월 14일 14시 탑솔라 본사 대면 미팅용 협상 브리핑 준비\n" +
		"### 도구 결과 (Tool Outcomes)\n" +
		"- [gmail] 탑솔라 김민준 회신 수신(6/5)\n" +
		"- [asr] 6/6 통화 녹음 전사 완료\n"
}

// fixtureSummaryBad is a deliberately poor summary (drops most facts, adds a
// hallucinated number) — the "bad" case for the deterministic-scorer test.
func fixtureSummaryBad() string {
	return fmt.Sprintf("어떤 회사와 계약 얘기를 했고 미팅을 잡았습니다. 금액은 대략 %s 정도입니다.", "5억")
}
