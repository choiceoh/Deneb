package chat

import (
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// recallTemporalBoost multiplies the score of an evidence row whose timestamp
// falls inside a temporal frame the user named in the query ("지난달", "6월").
//
// This is the Deneb-shaped slice of RaMem (arXiv 2606.22844): the recall stack
// already carries source/provenance/staleness context, but it never scoped
// retrieval by the query's *temporal frame*, so a content-similar memory from
// the wrong episode (same entity, different month) could outrank the one the
// user actually meant — RaMem's "context collapse". The fix stays a soft boost,
// not a hard filter: a frameless query is left untouched and a strongly matching
// out-of-frame row can still surface, mirroring RaMem's discipline of falling
// back to content when the recall frame cannot be reliably grounded.
const recallTemporalBoost = 1.6

// recallMonthRe matches a concrete sino-Korean month ("6월", "지난 11월"). It
// requires digits immediately before 월 so it never fires on 월급(salary),
// 월요일(Monday), or 3개월(a duration: the 월 is preceded by 개, not the digit).
var recallMonthRe = regexp.MustCompile(`(\d{1,2})\s*월`)

// recallTemporalRange is an inclusive [From, To] unix-milli window parsed from an
// explicit temporal cue in the query. ok=false means no groundable cue, so
// callers leave ranking untouched (content-only) — a frameless query is never
// scoped.
type recallTemporalRange struct {
	From int64 // unix milli, inclusive
	To   int64 // unix milli, inclusive
	ok   bool
}

// parseRecallTemporalRange detects an explicit, unambiguous Korean temporal cue
// in the message and resolves it to a date window on the user's configured zone.
func parseRecallTemporalRange(message string) recallTemporalRange {
	return parseRecallTemporalRangeAt(message, dentime.Now())
}

// parseRecallTemporalRangeAt is parseRecallTemporalRange with an injected clock,
// so the cue→window mapping is unit-testable without touching the process zone.
// Only cues that name a concrete frame are honored; vague ones ("그때", "예전에")
// return ok=false so recall stays content-only.
func parseRecallTemporalRangeAt(message string, now time.Time) recallTemporalRange {
	m := strings.ReplaceAll(message, " ", "") // "지난 달" == "지난달"

	switch {
	// Single days (오늘 is intentionally omitted: too frequent, and already
	// served by the diary recency weighting).
	case strings.Contains(m, "그저께"), strings.Contains(m, "그제"):
		d := now.AddDate(0, 0, -2)
		return dayRange(d, d)
	case strings.Contains(m, "어제"):
		d := now.AddDate(0, 0, -1)
		return dayRange(d, d)

	// Weeks (Monday-anchored).
	case strings.Contains(m, "지난주"), strings.Contains(m, "저번주"):
		mon := weekMonday(now).AddDate(0, 0, -7)
		return dayRange(mon, mon.AddDate(0, 0, 6))
	case strings.Contains(m, "이번주"):
		return dayRange(weekMonday(now), now)

	// Months (지난달/이번달 use the native 달, distinct from sino 월 below).
	case strings.Contains(m, "지난달"), strings.Contains(m, "저번달"):
		lastDay := monthFirst(now).AddDate(0, 0, -1)
		return dayRange(monthFirst(lastDay), lastDay)
	case strings.Contains(m, "이번달"):
		return dayRange(monthFirst(now), now)

	// Years.
	case strings.Contains(m, "재작년"):
		return yearRange(now.Year()-2, now.Location())
	case strings.Contains(m, "작년"), strings.Contains(m, "지난해"):
		return yearRange(now.Year()-1, now.Location())
	case strings.Contains(m, "올해"), strings.Contains(m, "금년"):
		return dayRange(yearFirst(now), now)
	}

	// Sino-Korean "N월": the most recent past occurrence of that month. A month
	// at or before the current one is this year; a later month is last year.
	if mt := recallMonthRe.FindStringSubmatch(m); mt != nil {
		mo := atoiSafe(mt[1])
		if mo >= 1 && mo <= 12 {
			year := now.Year()
			if mo > int(now.Month()) {
				year--
			}
			first := time.Date(year, time.Month(mo), 1, 0, 0, 0, 0, now.Location())
			last := first.AddDate(0, 1, 0).AddDate(0, 0, -1)
			return dayRange(first, last)
		}
	}

	return recallTemporalRange{ok: false}
}

// dayRange returns the inclusive window spanning from's 00:00 to to's 23:59:59.999
// in their (shared) zone.
func dayRange(from, to time.Time) recallTemporalRange {
	return recallTemporalRange{From: dayStartMs(from), To: dayEndMs(to), ok: true}
}

func yearRange(year int, loc *time.Location) recallTemporalRange {
	first := time.Date(year, time.January, 1, 0, 0, 0, 0, loc)
	last := time.Date(year, time.December, 31, 0, 0, 0, 0, loc)
	return dayRange(first, last)
}

func dayStartMs(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).UnixMilli()
}

func dayEndMs(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, int(999*time.Millisecond), t.Location()).UnixMilli()
}

// weekMonday returns the Monday of t's week (ISO: Monday is the first day).
func weekMonday(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7 // Sun=0→6, Mon=1→0, …, Sat=6→5
	return t.AddDate(0, 0, -offset)
}

func monthFirst(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, t.Location())
}

func yearFirst(t time.Time) time.Time {
	return time.Date(t.Year(), time.January, 1, 0, 0, 0, 0, t.Location())
}

func atoiSafe(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
