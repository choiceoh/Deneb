package proactive

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func TestCleanLLMCardTitleRejectsGenericOrShortTitles(t *testing.T) {
	cases := map[string]string{
		"현대차 울산 가견적서 재송부":                            "현대차 울산 가견적서 재송부",    // kept as-is (no hard length clamp)
		"\"무림 과업지시서\"":                               "무림 과업지시서",           // surrounding quotes
		"## 📬 무림 지시서":                                "📬 무림 지시서",           // markdown heading
		"제목: 케이블 발주\n부가 설명은 무시한다":                    "제목: 케이블 발주",         // first line only
		"  \t 「JOCA 가격 확인」  ":                        "JOCA 가격 확인",         // CJK quotes + whitespace
		"메일제목이아주아주많이길어서넘쳐버림":                         "메일제목이아주아주많이길어서넘쳐버림", // long title kept intact (no clamp)
		"메일 분석 리포트":                                  "",                   // generic echo → reject (fallback)
		"음":                                          "",                   // too short → reject
		"":                                           "",                   // empty
		"<think>":                                    "",                   // thinking tag leak
		"We need answer in Korean exactly two lines": "",                   // English CoT leak
		"我们根据要求输出两行：标题和摘要":                           "",                   // Chinese CoT leak
		"우리는 입력 텍스트를 기반으로 제목과 요약을 추출해야 합니다": "", // Korean CoT leak
	}
	for in, want := range cases {
		if got := cleanLLMCardTitle(in); got != want {
			t.Errorf("cleanLLMCardTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

// The eleven leaked titles found in the live feed on 2026-09-06 (60 cards
// listed): the tiny titler thinking out loud after the label, or echoing its
// own instructions. A recoverable noun phrase in front of the tell is kept;
// everything else is rejected so the heuristic title applies.
func TestCleanLLMCardTitleSalvagesOrRejectsInlineReasoningLeaks(t *testing.T) {
	cases := map[string]string{
		`지앤비 EPC LOI 날인 요청 건? Need concise. Maybe "지앤비 EPC LOI 3건 날인 요청" count?`:  "지앤비 EPC LOI 날인 요청 건",
		`"안동 임하댐 수상 트레이·모듈 포지션 공유" maybe too long? Count: 안동(2) 임하댐(3) 수상(2) 트레이`: "안동 임하댐 수상 트레이·모듈 포지션 공유",
		`"당진 솔라빌리지 하도급 해지 방침" 정도가 적절할 것 같다. 20자 이내인지 확인: "당진 솔라빌리지 하도급 해지 방침"은 약`: "당진 솔라빌리지 하도급 해지 방침",
		"핵심 명사구, 한글 20자 이내, 군더더기 단어 없이.":                                          "",
		"핵심 명사구, 20자 이내, no filler.":                                     "",
		`Korean within 20 characters, no filler words like "메일 분석" etc.`: "",
		"Korean, within": "",
		"must be Korean within 20 characters, noun phrase, no filler words like": "",
		`Korean, within 20 characters, noun phrase, no filler words like "메일 분석`: "",
		"... /":                 "",
		strings.Repeat("가", 41): "", // twice the contract length = narration
		// Legit titles pass untouched, including punctuation and English brand names.
		"SunKean 케이블 2차 선적 최종서류 도착 — 확인 회신 필요": "SunKean 케이블 2차 선적 최종서류 도착 — 확인 회신 필요",
		"Xpanner X1 패널리프트":       "Xpanner X1 패널리프트",
		"파인드그린, 잔여 납부금 25.2억 통보": "파인드그린, 잔여 납부금 25.2억 통보",
	}
	for in, want := range cases {
		if got := cleanLLMCardTitle(in); got != want {
			t.Errorf("cleanLLMCardTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanLLMCardSummaryRejectsInstructionEcho(t *testing.T) {
	if got := cleanLLMCardSummary("요약은 카드 미리보기용으로 2문장(약 80자) 이내. 제목을 반복하지 말고"); got != "" {
		t.Errorf("instruction echo must be rejected, got %q", got)
	}
	if got := cleanLLMCardSummary("고려전선 케이블 18드럼이 차주 입고로 밀렸다. 현장 일정 재조정 필요."); got == "" {
		t.Error("a real summary must survive")
	}
}

// TestRelay_CardTitlerLLMWinsOrFallbackHeuristic verifies the lightweight-LLM
// titler names mail-report cards, is skipped for non-mail proactive cards,
// and falls back to the deterministic heuristic when the model returns "".
func TestRelay_CardTitlerLLMWinsOrFallbackHeuristic(t *testing.T) {
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

	t.Run("LLM summary can win while title falls back", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler: func(string) (string, string) {
				return "", "착수신고가 지연돼 오늘 중 회신이 필요합니다."
			},
		}
		if _, err := d.relayNative(mailBody); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if got := feedTitle(feed); got == "" || got == "메일 리포트" {
			t.Fatalf("title = %q, want heuristic fallback title", got)
		}
		if got := feedSummary(feed); got != "착수신고가 지연돼 오늘 중 회신이 필요합니다." {
			t.Fatalf("summary = %q, want the LLM summary", got)
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

	t.Run("non-mail card is also LLM-titled", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		called := false
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) (string, string) { called = true; return "대한전선 오후 회의", "" },
		}
		if _, err := d.relayNative("## 📅 오늘 일정\n\n- 14:00 대한전선 회의"); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if !called {
			t.Fatal("cardTitler was not called for a calendar body")
		}
		if got := feedTitle(feed); got != "대한전선 오후 회의" {
			t.Errorf("non-mail title = %q, want the LLM title", got)
		}
	})

	t.Run("empty LLM title keeps heuristic heading", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler:      func(string) (string, string) { return "", "" },
		}
		if _, err := d.relayNative("## 📅 오늘 일정\n\n- 14:00 대한전선 회의"); err != nil {
			t.Fatalf("relayNative: %v", err)
		}
		if got := feedTitle(feed); got != "📅 오늘 일정" {
			t.Errorf("fallback title = %q, want the heuristic heading", got)
		}
	})

	t.Run("mail notifier LLM-titles prompt-edited analysis", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		called := false
		d := proactiveRelayDeps{
			transcriptStore: newRecordingTranscriptStore(),
			workFeed:        feed,
			cardTitler: func(string) (string, string) {
				called = true
				return "JOCA 견적 회신", "6월 13일까지 단가 확인과 회신이 필요합니다."
			},
		}
		body := "긴급: JOCA Cable 견적 회신\n\n발신자: fred@example.com\n\n6/13까지 단가 확인이 필요합니다."
		if err := d.mailNotifierForSession(nativeWorkSessionKey).Notify(context.Background(), body); err != nil {
			t.Fatalf("mail notifier Notify: %v", err)
		}
		if !called {
			t.Fatal("cardTitler was not called for explicit mail notifier delivery")
		}
		if len(feed.items) != 1 {
			t.Fatalf("got %d work-feed item(s), want 1", len(feed.items))
		}
		it := feed.items[0]
		if it.Source != workfeed.SourceMailReport {
			t.Fatalf("source = %q, want %q", it.Source, workfeed.SourceMailReport)
		}
		if it.Title != "JOCA 견적 회신" {
			t.Fatalf("title = %q, want LLM title", it.Title)
		}
		if it.Summary != "6월 13일까지 단가 확인과 회신이 필요합니다." {
			t.Fatalf("summary = %q, want LLM summary", it.Summary)
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
			"unlabeled first line is ignored (not a title)",
			"케이블 발주 검토\n진영상사 발주 건을 오늘까지 확인해야 합니다.",
			"",
			"",
		},
		{
			"CoT preamble then labeled title",
			"We need answer in Korean exactly two lines.\n제목: 풍력 실측 재방문\n요약: 제안 일정은 아직 회신이 없습니다.",
			"풍력 실측 재방문",
			"제안 일정은 아직 회신이 없습니다.",
		},
		{
			"thinking tags stripped before labeled parse",
			"<think>We need a title</think>\n제목: 풍력 실측 재방문\n요약: 회신이 없습니다.",
			"풍력 실측 재방문",
			"회신이 없습니다.",
		},
		{
			"mid-line 제목 after CoT on the same line",
			"我们根据要求输出两行。제목: 풍력 실측 재방문 합의",
			"풍력 실측 재방문 합의",
			"",
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
