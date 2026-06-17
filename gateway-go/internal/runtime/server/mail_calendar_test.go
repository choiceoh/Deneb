package server

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

func TestCalendarProposalsFromMail(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.Local)
	items := []gmailpoll.ActionItem{
		{Title: "킥오프 미팅 참석", DueHint: "6월 30일", Priority: "high"}, // dated + high → proposed
		{Title: "자료 검토", DueHint: "", Priority: "high"},           // no date → skipped
		{Title: "사소한 확인", DueHint: "내일", Priority: "low"},         // dated but low → skipped
		{Title: "  ", DueHint: "내일", Priority: "high"},            // blank title → skipped
	}
	deal := &gmailpoll.DealInfo{Counterparty: "탑솔라", DocType: "세금계산서", DueDate: "2026-06-30"}

	got := calendarProposalsFromMail("m1", "FW: 미팅", "boss@example.com", items, deal, now)

	if len(got) != 2 {
		t.Fatalf("got %d proposals, want 2: %+v", len(got), got)
	}
	// 1) the high-priority dated meeting
	if got[0].Title != "킥오프 미팅 참석" || got[0].Kind != "meeting" {
		t.Errorf("proposal[0] = %+v", got[0])
	}
	if got[0].Start == "" || got[0].Source != "mail:m1|킥오프 미팅 참석" {
		t.Errorf("proposal[0] start/source = %q / %q", got[0].Start, got[0].Source)
	}
	// 2) the deal deadline
	if got[1].Title != "탑솔라 세금계산서 결제 기한" || got[1].Kind != "deadline" || !got[1].AllDay {
		t.Errorf("proposal[1] = %+v", got[1])
	}
	if got[1].Start != "2026-06-30" || got[1].Source != "mail:m1|deal-due" {
		t.Errorf("proposal[1] start/source = %q / %q", got[1].Start, got[1].Source)
	}
}

func TestCalendarProposalsFromMail_NoneWhenNothingQualifies(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.Local)
	items := []gmailpoll.ActionItem{
		{Title: "막연한 후속", DueHint: "", Priority: "high"}, // no date
		{Title: "낮은 우선순위", DueHint: "내일", Priority: "low"},
	}
	if got := calendarProposalsFromMail("m2", "s", "f", items, nil, now); len(got) != 0 {
		t.Fatalf("want 0 proposals, got %d: %+v", len(got), got)
	}
}

func TestDealDeadlineTitle(t *testing.T) {
	cases := []struct {
		deal gmailpoll.DealInfo
		want string
	}{
		{gmailpoll.DealInfo{Counterparty: "탑솔라", DocType: "세금계산서"}, "탑솔라 세금계산서 결제 기한"},
		{gmailpoll.DealInfo{Counterparty: "남도에코"}, "남도에코 결제 기한"},
		{gmailpoll.DealInfo{}, "결제 기한"},
	}
	for _, c := range cases {
		if got := dealDeadlineTitle(&c.deal); got != c.want {
			t.Errorf("dealDeadlineTitle(%+v) = %q, want %q", c.deal, got, c.want)
		}
	}
}

func TestParseTimeOfDay(t *testing.T) {
	cases := []struct {
		hint string
		h, m int
		ok   bool
	}{
		{"6월 15일 14:00", 14, 0, true},
		{"내일 오후 2시", 14, 0, true},
		{"오전 9시 30분", 9, 30, true},
		{"14시", 14, 0, true},
		{"오후 2:30", 14, 30, true},
		{"오전 12시", 0, 0, true},  // midnight
		{"6월 15일", 0, 0, false}, // date only, no time
		{"2시간 후", 0, 0, false},  // duration, not a clock time
		{"내일", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		h, m, ok := parseTimeOfDay(c.hint)
		if ok != c.ok {
			t.Errorf("parseTimeOfDay(%q) ok=%v want %v", c.hint, ok, c.ok)
			continue
		}
		if ok && (h != c.h || m != c.m) {
			t.Errorf("parseTimeOfDay(%q) = %d:%02d want %d:%02d", c.hint, h, m, c.h, c.m)
		}
	}
}

func TestCalendarProposalsFromMail_TimedMeeting(t *testing.T) {
	now := time.Date(2026, 6, 17, 10, 0, 0, 0, time.Local)
	items := []gmailpoll.ActionItem{
		// medium priority but TIMED → should still be proposed (a real meeting),
		// and as a timed (not all-day) event.
		{Title: "주간 회의 참석", DueHint: "6월 18일 14:00", Priority: "medium"},
	}
	got := calendarProposalsFromMail("m1", "회의", "boss@example.com", items, nil, now)
	if len(got) != 1 {
		t.Fatalf("want 1 proposal, got %d: %+v", len(got), got)
	}
	if got[0].AllDay {
		t.Error("a timed meeting must not be all-day")
	}
	// Start is RFC3339 with the 14:00 time.
	if !strings.Contains(got[0].Start, "T14:00") {
		t.Errorf("expected 14:00 in start, got %q", got[0].Start)
	}
}
