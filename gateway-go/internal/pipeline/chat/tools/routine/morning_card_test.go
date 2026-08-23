package routine

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
)

func morningCardFixture() morningLetterEnvelope {
	return morningLetterEnvelope{
		Date:      "2026년 7월 18일 토요일",
		Timestamp: "2026-07-18T08:00:00+09:00",
		Note:      morningLetterNote,
		Sections: morningLetterSections{
			Weather: weatherData{
				OK: true, TempC: "27", FeelsLikeC: "29", Condition: "Light rain",
				Humidity: "82", MinTempC: "24", MaxTempC: "31", MaxRainPct: 70,
			},
			Exchange: exchangeData{OK: true, USDKRW: 1388.6},
			Copper:   copperData{OK: true, PricePerTon: 9876.4, Display: "9,876", Date: "2026-07-17"},
			Calendar: calendarData{OK: true, Events: []string{"07/18 09:00 — 현장 점검", "07/18 14:00 — 계약 검토"}},
			ProjectSignals: morningProjectSignalsData{OK: true, Items: []morningProjectSignal{
				{Title: "비금도 154kV", Updated: "2026-07-18", DoneLine: "견적 회신 수신", PlannedLine: "계약 조건 검토"},
			}},
			Email: emailData{OK: true, Messages: []emailEntry{
				{From: `김부장 <kim@example.com>`, Subject: "비금도 견적 회신 요청"},
				{From: "공격자", Subject: "코드 ```block``` & </text> 삽입"},
			}},
			Deadlines: deadlineData{OK: true, Items: []deadlineEntry{
				{Title: "진코 선입금 상계", Due: "2026-07-17", DaysLeft: -1},
				{Title: "부가세 신고", Due: "2026-07-20", DaysLeft: 2},
			}},
			OpenQuestions: openQuestionsData{OK: true, Items: []wikiport.OpenQuestion{
				{Project: "영광", Question: "계통 연계 답변 주체 확인", AgeDays: 9},
			}},
			GroupwarePending: groupwarePendingData{OK: true, Configured: true, Count: 2, StaleCount: 1, Items: []groupwarePendingEntry{
				{Title: "지출품의", Drafter: "김승리", EscalationLevel: 1, StaleLabel: "4시간째 방치"},
			}},
			GroupwareCC: groupwareCCData{OK: true, Configured: true, Count: 1, Items: []groupwareCCEntry{
				{Title: "수신참조 문서", Gist: "납기 변경 공유"},
			}},
		},
	}
}

func TestRenderMorningLetterCardUsesModelSlotsInsideFixedFormat(t *testing.T) {
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, kstLocation)
	env := morningCardFixture()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	narrative := `{"headline":"비금도 계약 조건 확인이 오늘의 핵심입니다.","weather_note":"오후 비가 예상돼 외근 이동 시간을 여유 있게 잡으세요.","projects":[{"title":"비금도 154kV","priority":"due","what_happened":"견적 회신을 받아 계약 조건 검토 단계로 넘어갔습니다.","why_important":"발주 일정과 원가 확정에 직접 연결됩니다.","next_action":"회신 조건을 검토하고 이견을 정리하세요."}],"risks":["계약 조건 확정 지연 시 발주 일정이 밀릴 수 있습니다."],"suggestions":["오전 중 이견 목록을 확정하세요."]}`

	msg, err := RenderMorningLetterCard(string(raw), narrative, now)
	if err != nil {
		t.Fatalf("RenderMorningLetterCard: %v", err)
	}
	fences := denebui.ExtractFences(msg)
	if len(fences) != 1 {
		t.Fatalf("fences=%d", len(fences))
	}
	issues, err := denebui.Validate(fences[0])
	if err != nil || len(issues) != 0 {
		t.Fatalf("model-filled card invalid: err=%v issues=%v", err, issues)
	}
	for _, want := range []string{
		"비금도 계약 조건 확인이 오늘의 핵심",
		`<badge color="warning">마감임박</badge>`,
		"발주 일정과 원가 확정에 직접 연결",
		"⚠️ 계약 조건 확정 지연",
		"💡 오전 중 이견 목록",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("model-filled card missing %q", want)
		}
	}
	if strings.Contains(msg, "새 메일 신호") || strings.Contains(msg, "최근 프로젝트 맥락") {
		t.Error("raw fallback sections must not duplicate model-grouped projects")
	}
}

func TestRenderMorningLetterCardInvalidNarrativeFallsBackToFacts(t *testing.T) {
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, kstLocation)
	raw, err := json.Marshal(morningCardFixture())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := RenderMorningLetterCard(string(raw), "not json", now)
	if err != nil {
		t.Fatalf("invalid narrative should be soft fallback: %v", err)
	}
	if !strings.Contains(msg, "최근 프로젝트 맥락") || !strings.Contains(msg, "새 메일 신호") {
		t.Fatalf("facts fallback missing: %s", msg)
	}
}

func TestRenderMorningLetterCardEmptyModelProjectsFallBackToFacts(t *testing.T) {
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, kstLocation)
	raw, err := json.Marshal(morningCardFixture())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := RenderMorningLetterCard(string(raw), `{"projects":[{"title":"  "}]}`, now)
	if err != nil {
		t.Fatalf("empty project should be a soft fallback: %v", err)
	}
	if !strings.Contains(msg, "최근 프로젝트 맥락") || !strings.Contains(msg, "새 메일 신호") {
		t.Fatalf("facts fallback missing: %s", msg)
	}
}

func TestComposeMorningLetterCardIsDeliveryReadyAndSchemaValid(t *testing.T) {
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, kstLocation)
	msg := composeMorningLetterCard(morningCardFixture(), now)

	if !strings.HasPrefix(msg, "좋은 아침이에요 — 2026년 7월 18일 토요일. 🔴 진코 선입금 상계 기한 초과") {
		t.Fatalf("priority head missing: %q", msg[:min(len(msg), 120)])
	}
	fences := denebui.ExtractFences(msg)
	if len(fences) != 1 {
		t.Fatalf("expected exactly one deneb-ui fence, got %d", len(fences))
	}
	issues, err := denebui.Validate(fences[0])
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("card must be schema-valid: %v", issues)
	}

	for _, want := range []string{
		`<stat value="1,389" label="USD/KRW"/>`,
		`<stat value="9,876 /t (2026-07-17)" label="LME 구리"/>`,
		`<badge color="error">기한 초과</badge>`,
		`<badge color="warning">D-2</badge>`,
		"🟠 방치 결재 1건",
		"비금도 154kV — 견적 회신 수신 · 다음: 계약 조건 검토",
		"'''block''' &amp; &lt;/text>",
	} {
		if !strings.Contains(fences[0], want) {
			t.Errorf("card missing %q", want)
		}
	}
	if strings.Contains(msg, "{{market:") || strings.Contains(msg, "EUR") {
		t.Errorf("delivery must contain direct server-formatted USD/copper values only: %s", msg)
	}
}

func TestComposeMorningLetterCardTodayFocusAndQuoteGroups(t *testing.T) {
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, kstLocation)
	env := morningCardFixture()
	env.Sections.Email.Messages = []emailEntry{
		{From: "김부장", Subject: "기아 AL 광주 1차 견적"},
		{From: "김부장", Subject: "기아 AL 광주 재견적"},
		{From: "김부장", Subject: "기아 AL 이천 가견적"},
		{From: "영업", Subject: "주간 회의 안내"},
	}
	msg := composeMorningLetterCard(env, now)
	fences := denebui.ExtractFences(msg)
	if len(fences) != 1 {
		t.Fatalf("fences=%d", len(fences))
	}
	issues, err := denebui.Validate(fences[0])
	if err != nil || len(issues) != 0 {
		t.Fatalf("card invalid: err=%v issues=%v", err, issues)
	}
	for _, want := range []string{
		"오늘 전무 할 일",
		"진코 선입금 상계 — 기한 초과",
		"07/18 09:00 — 현장 점검",
		"지출품의 — 4시간째 방치",
		"견적 묶음 · 광주 · 2건",
		"기아 AL 광주 재견적",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("card missing %q", want)
		}
	}
	if strings.Contains(msg, "견적 묶음 · 이천") {
		t.Error("single-site quote must not get its own bundle card")
	}
	if !strings.Contains(msg, "새 메일 신호") {
		t.Error("raw email list must stay even when quotes are grouped")
	}
}

func TestCollectMorningTodayFocusSkipsTomorrowAndDistantDeadlines(t *testing.T) {
	now := time.Date(2026, 8, 24, 8, 0, 0, 0, kstLocation)
	got := collectMorningTodayFocus(
		calendarData{OK: true, Events: []string{"08/24 09:00 — 오전 회의", "08/25 10:00 — 내일 일"}},
		deadlineData{Items: []deadlineEntry{
			{Title: "기아 광주 재견적", DaysLeft: 0},
			{Title: "부가세", DaysLeft: 3},
		}},
		groupwarePendingData{Items: []groupwarePendingEntry{
			{Title: "신선 결재", EscalationLevel: 0},
			{Title: "방치 품의", EscalationLevel: 1, StaleLabel: "어제부터"},
		}},
		now,
	)
	joined := strings.Join(got, "\n")
	for _, want := range []string{"기아 광주 재견적", "08/24 09:00 — 오전 회의", "방치 품의"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %q", want, joined)
		}
	}
	for _, unwanted := range []string{"부가세", "내일 일", "신선 결재"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("unexpected %q in %q", unwanted, joined)
		}
	}
	if len(got) > morningTodayFocusMax {
		t.Fatalf("len=%d, want <= %d", len(got), morningTodayFocusMax)
	}
}

func TestGroupMorningQuoteMailsRequiresTwoPerSite(t *testing.T) {
	groups := groupMorningQuoteMails([]emailEntry{
		{From: "A", Subject: "기아 AL 광주 견적"},
		{From: "A", Subject: "기아 AL 광주 재산정"},
		{From: "A", Subject: "기아 AL 이천 견적"},
		{From: "A", Subject: "주간 보고"},
	})
	if len(groups) != 1 || groups[0].Site != "광주" || len(groups[0].Items) != 2 {
		t.Fatalf("got %+v", groups)
	}
}

func TestWriteMorningWeatherShowsUnknownCondition(t *testing.T) {
	var b strings.Builder
	writeMorningWeather(&b, weatherData{OK: true, TempC: "23", FeelsLikeC: "27", Humidity: "96"}, "")
	if !strings.Contains(b.String(), "상태 미확인") {
		t.Fatalf("empty condition should surface: %s", b.String())
	}
}

func TestComposeMorningLetterCardAllFailuresStillReturnsMinimumCard(t *testing.T) {
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, kstLocation)
	msg := composeMorningLetterCard(morningLetterEnvelope{
		Date: "2026년 7월 18일 토요일",
		Sections: morningLetterSections{
			Weather: weatherData{}, Exchange: exchangeData{}, Copper: copperData{},
			Calendar: calendarData{}, ProjectSignals: morningProjectSignalsData{}, Email: emailData{}, Deadlines: deadlineData{},
			OpenQuestions: openQuestionsData{}, GroupwarePending: groupwarePendingData{}, GroupwareCC: groupwareCCData{},
		},
	}, now)
	fences := denebui.ExtractFences(msg)
	if len(fences) != 1 {
		t.Fatalf("fences=%d, want 1", len(fences))
	}
	issues, err := denebui.Validate(fences[0])
	if err != nil || len(issues) != 0 {
		t.Fatalf("minimum card invalid: err=%v issues=%v", err, issues)
	}
	if got := strings.Count(fences[0], "조회 실패"); got < 4 {
		t.Errorf("failure placeholders=%d, want at least 4", got)
	}
}
