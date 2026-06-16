package server

import (
	"strings"
	"testing"
)

func TestCleanLLMCardTitle(t *testing.T) {
	cases := map[string]string{
		"현대차 울산 가견적서 재송부":         "현대차 울산 가견적서 재송부",     // 15 runes, kept as-is (within the 16 limit)
		"\"무림 과업지시서\"":            "무림 과업지시서",            // surrounding quotes
		"## 📬 무림 지시서":             "📬 무림 지시서",            // markdown heading
		"제목: 케이블 발주\n부가 설명은 무시한다": "제목: 케이블 발주",          // first line only
		"  \t 「JOCA 가격 확인」  ":     "JOCA 가격 확인",          // CJK quotes + whitespace
		"메일제목이아주아주많이길어서넘쳐버림":      "메일제목이아주아주많이길어서넘쳐...", // >16 runes → clamped to 16 + "..."
		"메일 분석 리포트":               "",                    // generic echo → reject (fallback)
		"음":                       "",                    // too short → reject
		"":                        "",                    // empty
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
			cardTitler:      func(string) (string, string) { return "무림 착수신고 확인", "" },
		}
		if _, err := d.relayNative(mailBody); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if len(feed.items) != 1 || feed.items[0].Title != "무림 착수신고 확인" {
			t.Fatalf("title = %q, want the LLM title", feedTitle(feed))
		}
	})

	t.Run("LLM summary replaces the heuristic when present", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler: func(string) (string, string) {
				return "무림 착수신고 확인", "착수신고가 지연돼 오늘 중 회신이 필요합니다."
			},
		}
		if _, err := d.relayNative(mailBody); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if got := feedSummary(feed); got != "착수신고가 지연돼 오늘 중 회신이 필요합니다." {
			t.Fatalf("summary = %q, want the LLM summary", got)
		}
	})

	t.Run("empty LLM summary keeps the heuristic summary", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) (string, string) { return "무림 착수신고 확인", "" },
		}
		if _, err := d.relayNative(mailBody); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if got := feedSummary(feed); got == "" {
			t.Fatalf("summary is empty, want the heuristic summary as fallback")
		}
	})

	t.Run("falls back to heuristic subject when LLM returns empty", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) (string, string) { return "", "" },
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
			cardTitler:      func(string) (string, string) { return "LG 결재 지연 정리", "" },
		}
		// Opens with a narration sentence (no heading): the heuristic would grab the
		// whole sentence, so the lightweight titler names it instead.
		if _, err := d.relayNative("이제 자료가 다 모였다. 놀랍게도 6/10에 지연됐던 LG 내부 결재가 통과됐다."); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if got := feedTitle(feed); got != "LG 결재 지연 정리" {
			t.Fatalf("prose title = %q, want the LLM title", got)
		}
	})

	t.Run("non-mail card is not LLM-titled", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		called := false
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) (string, string) { called = true; return "SHOULD NOT BE USED", "" },
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

func feedSummary(f *recordingWorkFeed) string {
	if len(f.items) == 0 {
		return ""
	}
	return f.items[0].Summary
}

func TestParseLLMTitleSummary(t *testing.T) {
	cases := []struct {
		name        string
		raw         string
		wantTitle   string
		wantSummary string
	}{
		{
			"labeled 제목 / 요약",
			"제목: 무림 착수신고 확인\n요약: 착수신고가 지연돼 오늘 중 회신이 필요합니다.",
			"무림 착수신고 확인",
			"착수신고가 지연돼 오늘 중 회신이 필요합니다.",
		},
		{
			"unlabeled: first line title, rest summary",
			"케이블 발주 검토\n진영상사 발주 건을 오늘까지 확인해야 합니다.",
			"케이블 발주 검토",
			"진영상사 발주 건을 오늘까지 확인해야 합니다.",
		},
		{
			"markdown + quotes stripped from both",
			"제목: **\"광명역 배치도\"**\n요약: - 배치도 송부 건, 회신 필요",
			"광명역 배치도",
			"배치도 송부 건, 회신 필요",
		},
		{
			"title only (no summary line) → empty summary",
			"제목: 단가 확인",
			"단가 확인",
			"",
		},
		{
			"generic title rejected, summary still returned",
			"제목: 메일 분석 리포트\n요약: 무림피앤피 과업지시서가 도착했습니다.",
			"",
			"무림피앤피 과업지시서가 도착했습니다.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTitle, gotSummary := parseLLMTitleSummary(tc.raw)
			if gotTitle != tc.wantTitle {
				t.Errorf("title = %q, want %q", gotTitle, tc.wantTitle)
			}
			if gotSummary != tc.wantSummary {
				t.Errorf("summary = %q, want %q", gotSummary, tc.wantSummary)
			}
		})
	}
}
