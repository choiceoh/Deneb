package server

import (
	"strings"
	"testing"
)

func TestCleanLLMCardTitle(t *testing.T) {
	cases := map[string]string{
		"현대차 울산공장 태양광 가견적서 재송부":     "현대차 울산공장 태양광 가견적서 재송부",
		"\"무림피앤피 과업지시서 송부\"":        "무림피앤피 과업지시서 송부",  // surrounding quotes
		"## 📬 무림피앤피 과업지시서":          "📬 무림피앤피 과업지시서",   // markdown heading
		"제목: 솔라케이블 발주\n부가 설명은 무시한다": "제목: 솔라케이블 발주",    // first line only
		"  \t 「JOCA 케이블 가격 재확인」  ":  "JOCA 케이블 가격 재확인", // CJK quotes + whitespace
		"메일 분석 리포트":                 "",                // generic echo → reject (fallback)
		"음":                         "",                // too short → reject
		"":                          "",                // empty
	}
	for in, want := range cases {
		if got := cleanLLMCardTitle(in); got != want {
			t.Errorf("cleanLLMCardTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRelay_CardTitler verifies the lightweight-LLM titler names mail-report
// cards, is skipped for non-mail proactive cards, and falls back to the
// deterministic heuristic when the model returns "".
func TestRelay_CardTitler(t *testing.T) {
	mailBody := "## 📬 메일 분석 리포트\n\n### 무림피앤피 울산공장 과업지시서 송부\n**🟡 확인 필요**"

	t.Run("LLM title wins for a mail report", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) string { return "무림 과업지시서 — 착수신고 확인 필요" },
		}
		if _, err := d.relayNative(mailBody); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if len(feed.items) != 1 || feed.items[0].Title != "무림 과업지시서 — 착수신고 확인 필요" {
			t.Fatalf("title = %q, want the LLM title", feedTitle(feed))
		}
	})

	t.Run("falls back to heuristic subject when LLM returns empty", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) string { return "" },
		}
		if _, err := d.relayNative(mailBody); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		got := feedTitle(feed)
		if !strings.Contains(got, "무림피앤피") || got == "📬 메일 분석 리포트" {
			t.Fatalf("fallback title = %q, want the heuristic subject", got)
		}
	})

	t.Run("prose-opening proactive card is LLM-titled", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) string { return "LG 내부 결재 지연 정리" },
		}
		// Opens with a narration sentence (no heading): the heuristic would grab the
		// whole sentence, so the lightweight titler names it instead.
		if _, err := d.relayNative("이제 자료가 다 모였다. 놀랍게도 6/10에 지연됐던 LG 내부 결재가 통과됐다."); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if got := feedTitle(feed); got != "LG 내부 결재 지연 정리" {
			t.Fatalf("prose title = %q, want the LLM title", got)
		}
	})

	t.Run("non-mail card is not LLM-titled", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		called := false
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) string { called = true; return "SHOULD NOT BE USED" },
		}
		if _, err := d.relayNative("## 📅 오늘 일정\n\n- 14:00 대한전선 회의"); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if called {
			t.Error("cardTitler was called for a non-mail (calendar) body")
		}
		if got := feedTitle(feed); got != "📅 오늘 일정" {
			t.Errorf("non-mail title = %q, want the heuristic heading", got)
		}
	})
}

func feedTitle(f *recordingWorkFeed) string {
	if len(f.items) == 0 {
		return ""
	}
	return f.items[0].Title
}
