package tools

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

func TestFormatLetterCalendar(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 6, 28, h, m, 0, 0, kstLocation) }
	events := []calendar.Event{
		{Summary: "팀 스탠드업", Start: at(9, 0)},
		{Summary: "거래처 미팅", Location: "본사", Start: at(14, 0)},
	}
	got := formatLetterCalendar(events, 10)
	want := []string{
		"06/28 09:00 — 팀 스탠드업",
		"06/28 14:00 — 거래처 미팅 @본사",
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFormatLetterCalendar_CapAndEmpty(t *testing.T) {
	if got := formatLetterCalendar(nil, 10); len(got) != 0 {
		t.Errorf("nil events should yield empty, got %v", got)
	}

	base := time.Date(2026, 6, 28, 8, 0, 0, 0, kstLocation)
	var many []calendar.Event
	for i := 0; i < 15; i++ {
		many = append(many, calendar.Event{Summary: "evt", Start: base.Add(time.Duration(i) * time.Hour)})
	}
	if got := formatLetterCalendar(many, 10); len(got) != 10 {
		t.Errorf("cap not applied: len = %d, want 10", len(got))
	}
}
