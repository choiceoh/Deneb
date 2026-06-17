package handlerminiapp

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calprop"
)

func TestProposalTimes_AllDayDateStable(t *testing.T) {
	// A local-midnight all-day instant serializes with a TZ offset that can roll
	// to the prior day in UTC; noon-anchoring keeps the date stable. Regression
	// for a 6/20 proposal that created a 6/19 event.
	p := &calprop.Proposal{Start: "2026-06-20", AllDay: true}
	start, end, allDay, err := proposalTimes(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !allDay {
		t.Fatal("expected allDay")
	}
	if got := start.Format("2006-01-02"); got != "2026-06-20" {
		t.Errorf("start date drifted: %s", got)
	}
	if start.Hour() != 12 {
		t.Errorf("expected local noon, got hour %d", start.Hour())
	}
	if !end.After(start) {
		t.Error("end must be after start")
	}
}

func TestProposalTimes_Timed(t *testing.T) {
	p := &calprop.Proposal{Start: "2026-06-20T14:00:00+09:00", AllDay: false}
	start, end, allDay, err := proposalTimes(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if allDay {
		t.Error("expected timed, not allDay")
	}
	if end.Sub(start) != time.Hour {
		t.Errorf("expected 1h default duration, got %v", end.Sub(start))
	}
}

func TestProposalTimes_BadStart(t *testing.T) {
	if _, _, _, err := proposalTimes(&calprop.Proposal{Start: "nonsense", AllDay: true}); err == nil {
		t.Error("expected error for unparseable all-day date")
	}
	if _, _, _, err := proposalTimes(&calprop.Proposal{Start: "nope", AllDay: false}); err == nil {
		t.Error("expected error for unparseable timed start")
	}
}
