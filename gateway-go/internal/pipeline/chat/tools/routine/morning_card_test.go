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
