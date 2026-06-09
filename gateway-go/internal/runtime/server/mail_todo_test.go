package server

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

func TestTodosFromActionItems_HighPriorityOnly(t *testing.T) {
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	items := []gmailpoll.ActionItem{
		{Title: "계약서 검토", DueHint: "내일", Priority: "high"},
		{Title: "참고만 하면 됨", DueHint: "", Priority: "low"},
		{Title: "회신", DueHint: "3일 후", Priority: "medium"},
		{Title: "  ", Priority: "high"},                 // empty title → skipped
		{Title: "송금 승인", DueHint: "", Priority: "HIGH"}, // case-insensitive
	}
	got := todosFromActionItems("msg123", "6월 견적", "kim@example.com", items, now)
	if len(got) != 2 {
		t.Fatalf("expected 2 high-priority to-dos, got %d: %+v", len(got), got)
	}
	if got[0].Title != "계약서 검토" {
		t.Errorf("first title = %q, want 계약서 검토", got[0].Title)
	}
	if got[0].Due.IsZero() {
		t.Errorf("expected a due date resolved from 내일, got zero")
	}
	if !strings.Contains(got[0].Note, "6월 견적") || !strings.Contains(got[0].Note, "kim@example.com") {
		t.Errorf("note missing mail back-reference: %q", got[0].Note)
	}
	if got[0].Source != "mail:msg123|계약서 검토" {
		t.Errorf("source = %q, want mail:msg123|계약서 검토", got[0].Source)
	}
	if got[1].Title != "송금 승인" {
		t.Errorf("second title = %q, want 송금 승인 (HIGH normalized)", got[1].Title)
	}
}

func TestTodosFromActionItems_NoneWhenNoHigh(t *testing.T) {
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	items := []gmailpoll.ActionItem{
		{Title: "참고", Priority: "low"},
		{Title: "검토", Priority: "medium"},
	}
	if got := todosFromActionItems("m", "s", "f", items, now); got != nil {
		t.Errorf("expected nil (no high-priority), got %+v", got)
	}
}

func TestParseDueHint_Relative(t *testing.T) {
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	today := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		hint string
		want time.Time // zero = expect not-parsed
	}{
		{"오늘", today},
		{"내일까지", today.AddDate(0, 0, 1)},
		{"모레", today.AddDate(0, 0, 2)},
		{"3일 후", today.AddDate(0, 0, 3)},
		{"2주 뒤", today.AddDate(0, 0, 14)},
		{"5일 이내", today.AddDate(0, 0, 5)},
		{"다음 주", today.AddDate(0, 0, 7)},
		{"", time.Time{}},
		{"가능한 빨리", time.Time{}},
	}
	for _, c := range cases {
		got, allDay := parseDueHint(c.hint, now)
		if c.want.IsZero() {
			if !got.IsZero() {
				t.Errorf("parseDueHint(%q) = %v, want zero", c.hint, got)
			}
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("parseDueHint(%q) = %v, want %v", c.hint, got, c.want)
		}
		if !allDay {
			t.Errorf("parseDueHint(%q) allDay = false, want true", c.hint)
		}
	}
}

func TestParseDueHint_ExplicitDates(t *testing.T) {
	now := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)

	// ISO date, unambiguous.
	if got, _ := parseDueHint("2026-06-15 까지 회신", now); !got.Equal(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ISO date = %v, want 2026-06-15", got)
	}
	// Month/day in the future this year.
	if got, _ := parseDueHint("6월 15일", now); !got.Equal(time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("월/일 future = %v, want 2026-06-15", got)
	}
	// Month/day already past this year rolls to next year.
	if got, _ := parseDueHint("6월 1일", now); !got.Equal(time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("월/일 past = %v, want 2027-06-01 (rolled)", got)
	}
}

func TestEndOfWeek_AlwaysForwardFriday(t *testing.T) {
	base := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	for offset := range 14 {
		today := base.AddDate(0, 0, offset)
		eow := endOfWeek(today)
		if eow.Weekday() != time.Friday {
			t.Errorf("endOfWeek(%s, %v) weekday = %v, want Friday", today.Format("2006-01-02"), today.Weekday(), eow.Weekday())
		}
		if eow.Before(today) {
			t.Errorf("endOfWeek(%s) = %s is before today", today.Format("2006-01-02"), eow.Format("2006-01-02"))
		}
	}
}
