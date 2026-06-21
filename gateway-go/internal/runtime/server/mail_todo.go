// mail_todo.go — Korean due-hint / date parsing for mail-derived items.
//
// Mail no longer auto-creates to-dos (operator approval first — schedule-worthy
// follow-ups surface as calendar PROPOSALS via the bell, see mail_calendar.go).
// What remains here is the shared due-hint parser those proposals use.
package server

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- due-hint resolution -------------------------------------------------

var relativeDaysRe = regexp.MustCompile(`(\d+)\s*([일주])\s*(?:후|뒤|내|이내)`)

var (
	isoDateRe  = regexp.MustCompile(`(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})`)
	monthDayRe = regexp.MustCompile(`(\d{1,2})\s*월\s*(\d{1,2})\s*일`)
)

// parseDueHint resolves a free-text Korean/relative due cue to a date. Returns
// (zero, false) when the cue is empty or unrecognized — automation never
// invents a deadline it wasn't given. allDay is always true for the cues we
// parse: a to-do carries a date, not a wall-clock time.
func parseDueHint(hint string, now time.Time) (due time.Time, allDay bool) {
	h := strings.TrimSpace(hint)
	if h == "" {
		return time.Time{}, false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Explicit dates first so "6월 15일" / "2026-06-15" aren't mis-read as a
	// relative "15일" offset.
	if t, ok := parseExplicitDate(h, today); ok {
		return t, true
	}

	switch {
	case strings.Contains(h, "오늘"):
		return today, true
	case strings.Contains(h, "내일"):
		return today.AddDate(0, 0, 1), true
	case strings.Contains(h, "모레"):
		return today.AddDate(0, 0, 2), true
	case strings.Contains(h, "다음 주"), strings.Contains(h, "다음주"), strings.Contains(h, "차주"):
		return today.AddDate(0, 0, 7), true
	case strings.Contains(h, "이번 주"), strings.Contains(h, "이번주"), strings.Contains(h, "금주"):
		return endOfWeek(today), true
	}

	if d, ok := parseRelativeDays(h); ok {
		return today.AddDate(0, 0, d), true
	}
	return time.Time{}, false
}

// parseRelativeDays handles "3일 후", "2주 뒤", "5일 이내" → day offset. The
// trailing 후/뒤/내/이내 is required so a bare "15일" inside a date isn't taken
// as an offset.
func parseRelativeDays(h string) (int, bool) {
	m := relativeDaysRe.FindStringSubmatch(h)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	if m[2] == "주" {
		n *= 7
	}
	return n, true
}

// parseExplicitDate handles ISO (2026-06-15) and "6월 15일". A month/day with no
// year resolves to this year, rolling to next year if already past, so a new
// action never gets a due date in the past.
func parseExplicitDate(h string, today time.Time) (time.Time, bool) {
	if m := isoDateRe.FindStringSubmatch(h); m != nil {
		return ymd(atoiSafe(m[1]), atoiSafe(m[2]), atoiSafe(m[3]), today.Location())
	}
	if m := monthDayRe.FindStringSubmatch(h); m != nil {
		month, day := atoiSafe(m[1]), atoiSafe(m[2])
		t, ok := ymd(today.Year(), month, day, today.Location())
		if ok && t.Before(today) {
			t = t.AddDate(1, 0, 0)
		}
		return t, ok
	}
	return time.Time{}, false
}

// endOfWeek returns the Friday of today's week (KST work week), or today itself
// when it's already Friday or the weekend.
func endOfWeek(today time.Time) time.Time {
	offset := (int(time.Friday) - int(today.Weekday()) + 7) % 7
	return today.AddDate(0, 0, offset)
}

// ymd validates the month/day range loosely and returns a date-only time.
func ymd(y, m, d int, loc *time.Location) (time.Time, bool) {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc), true
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
