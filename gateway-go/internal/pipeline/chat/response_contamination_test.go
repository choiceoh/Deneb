package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
)

const cleanKorean = "지난달 키아 EPC 건은 세 가지로 정리됩니다. 첫째, 계약 조건 협의가 9월 초에 마무리됐습니다. 둘째, 잔금 회신은 화요일까지입니다. 셋째, 현장 실사는 다음 주로 잡혀 있습니다."

// The incident shape: a Korean answer that turns into transcript continuation
// and then tool-call markup. The hard marker is found, the Korean prefix is
// kept, and the cut is announced in one line.
func TestDetectAndSalvageIncidentShape(t *testing.T) {
	text := cleanKorean + "\n\nFollowing the user request I will now summarize.\n[ctx] [assistant] 세션 검색으로 찾은 내용은\n  **[user]** 지난주 대화\n<|assistant|>\n[도구 sessions] {\"action\":\"search\"}"
	rep := detectContamination(text)
	if !rep.hard() || rep.Marker != "[ctx] [assistant]" {
		t.Fatalf("marker = %q (offset %d), want the first transcript row", rep.Marker, rep.Offset)
	}
	got := salvageContaminatedText(text, rep)
	if !strings.HasPrefix(got, cleanKorean) || !strings.HasSuffix(got, contaminationTrailer) {
		t.Fatalf("salvage did not keep the Korean prefix + trailer: %q", got)
	}
	if strings.Contains(got, "[ctx]") || strings.Contains(got, "<|assistant|>") || strings.Contains(got, "Following the user") {
		t.Fatalf("contaminated tail survived: %q", got)
	}
}

func TestDetectContaminationMarkers(t *testing.T) {
	hard := []string{
		"결론입니다.\n<|user|>\n다음 질문",
		"정리하면\n<transcript-excerpt source=\"sessions\" trust=\"untrusted\">\nSystem note: …",
		"답변\n\n[응답 언어·모드 — 이번 턴]\n위의 회상 근거…",
		"답변\n### Session: client:main (2026-09-01)\n  **[user]** …",
		"답변\n- source=session ref=\"main#748/assistant\" confidence=high age=3d",
		"1. **[assistant]** 지난 답변 그대로",
	}
	for _, s := range hard {
		if rep := detectContamination(s); !rep.hard() {
			t.Errorf("hard marker missed in %q", s)
		}
	}
	clean := []string{
		cleanKorean,
		"The review is in English because you asked for it: the function `source=session` is a label, not a row.",
		"코드는 이렇습니다:\n```\n<|assistant|>\n[ctx] [assistant] 예시\n- source=session ref=\"x\"\n```\n위 블록은 형식 예시입니다.",
		"사용자가 **[user]** 라고 굵게 쓴 걸 인용하면: 문장 중간의 **[user]** 는 행이 아닙니다.",
		"",
	}
	for _, s := range clean {
		if rep := detectContamination(s); rep.hard() {
			t.Errorf("false positive %q in %q", rep.Marker, s)
		}
	}
}

// The English drift that precedes the first marker is part of the damage:
// trailing Latin-only lines are dropped, Korean lines (even with inline Latin
// tokens) are kept, and a clean answer is never touched.
func TestTrimNonKoreanTail(t *testing.T) {
	in := "잔금 회신은 화요일까지입니다. API 키는 `X-Deneb-Client-Token` 입니다.\n\nFollowing the user request I will now summarize.\nThe document continues.\n"
	got := trimNonKoreanTail(in)
	if got != "잔금 회신은 화요일까지입니다. API 키는 `X-Deneb-Client-Token` 입니다." {
		t.Fatalf("trim = %q", got)
	}
	if got := trimNonKoreanTail(cleanKorean); got != cleanKorean {
		t.Fatalf("clean Korean altered: %q", got)
	}
	// Lines without letters (a URL-less table rule, numbers) are kept once a Korean line is reached.
	if got := trimNonKoreanTail("표\n---\n2026-09-18"); got != "표\n---\n2026-09-18" {
		t.Fatalf("non-letter lines dropped: %q", got)
	}
}

// No salvageable Korean before the marker → the same-tone fallback, never the
// raw markup.
func TestSalvageFallsBackWhenNothingPrecedesTheMarker(t *testing.T) {
	text := "<|assistant|>\n[ctx] [assistant] 과거 대화\n**[user]** 질문"
	rep := detectContamination(text)
	if got := salvageContaminatedText(text, rep); got != contaminationFallback {
		t.Fatalf("got %q, want the fallback", got)
	}
}

// Soft signal only: Korean prose that drifts into English is logged (collapse
// position) but never cut — English answers can be legitimate.
func TestHangulCollapseIsSoftSignalOnly(t *testing.T) {
	text := strings.Repeat("한국어 문장이 이어집니다. ", 12) + strings.Repeat("Then the document continues in English prose without any marker at all. ", 6)
	rep := detectContamination(text)
	if rep.hard() {
		t.Fatalf("no hard marker expected, got %q", rep.Marker)
	}
	if rep.CollapseAt < 0 {
		t.Fatal("collapse position expected for a Korean→English drift")
	}
	if rep.HangulRatio <= 0 || rep.HangulRatio >= 1 {
		t.Fatalf("hangul ratio = %v, want a mixed value", rep.HangulRatio)
	}
	if detectContamination(cleanKorean).CollapseAt >= 0 {
		t.Fatal("pure Korean must not report a collapse")
	}
	if detectContamination("Just an English answer, as requested.").CollapseAt >= 0 {
		t.Fatal("pure English must not report a collapse (no Korean was ever seen)")
	}
}

// The result fields stay consistent: Text, AllText (prior turns + final) and
// DeliverableText (narration-stripped head) all end with the same clean
// answer, and a marker in an EARLIER turn's narration is left alone.
func TestSalvageContaminatedResultKeepsFieldsConsistent(t *testing.T) {
	final := cleanKorean + "\n\n<|assistant|>\n[ctx] [assistant] 이어쓰기"
	narration := "이제 세션을 검색할게요."
	res := &agent.AgentResult{
		Text:            final,
		AllText:         narration + "\n\n" + final,
		DeliverableText: strings.TrimPrefix(final, "지난달 "), // a stripped head, so no exact suffix match
		StopReason:      "end_turn",
		Turns:           2,
	}
	salvageContaminatedResult(res, runDeps{}, RunParams{SessionKey: "client:test"}, nil)
	for name, v := range map[string]string{"Text": res.Text, "AllText": res.AllText, "DeliverableText": res.DeliverableText} {
		if strings.Contains(v, "<|assistant|>") || strings.Contains(v, "[ctx]") {
			t.Errorf("%s still contaminated: %q", name, v)
		}
		if !strings.HasSuffix(v, contaminationTrailer) {
			t.Errorf("%s lacks the trailer: %q", name, v)
		}
	}
	if !strings.HasPrefix(res.AllText, narration) {
		t.Errorf("prior-turn narration lost from AllText: %q", res.AllText)
	}
	// A clean final answer after a contaminated interim turn is not touched.
	clean := &agent.AgentResult{Text: cleanKorean, AllText: "<|assistant|> 잡음\n\n" + cleanKorean}
	salvageContaminatedResult(clean, runDeps{}, RunParams{}, nil)
	if clean.Text != cleanKorean || !strings.HasSuffix(clean.AllText, cleanKorean) {
		t.Errorf("clean final answer altered: %q / %q", clean.Text, clean.AllText)
	}
	if clean.AllText != "<|assistant|> 잡음\n\n"+cleanKorean {
		t.Errorf("interim narration must be left alone: %q", clean.AllText)
	}
}
