package server

import (
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
