// deal_date.go — normalizing the 문서 일자 of a filed business document to ISO.
//
// DealRecord.Date is documented as "YYYY-MM-DD (or raw when unparseable)", but
// Korean business documents write the same date a dozen ways ("26.08.31",
// "2026. 6. 30.", "2026년 6월 29일", "2026-07-16 (목)"), and an Excel-sourced
// 견적서 can hand over a bare serial ("46254.54"). Filed verbatim, those are
// raw strings on the same field ISO dates live on, so every date-ranged query
// and every 월별 추이 either drops them or invents buckets by slicing the
// string ("2026. 6", "26.08.1"). The measured cost: 145 production rows, 58%
// non-ISO, none of them actually ambiguous.
//
// This file turns the unambiguous ones into ISO at file time. What stays raw is
// only what genuinely carries no single date: month-only forms ("2026-07"),
// unfilled contract placeholders ("2026년 [*]월 [*]일"), and relative deadlines
// ("계약 후 7일 이내"). The original text is never lost — dealRecordFrom keeps
// it in DateRaw whenever normalization changed it, the same audit contract
// AmountRaw holds for 금액.
package wiki

import (
	"log/slog"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dealDateCentury splits a two-digit year: 00–68 → 2000s, 69–99 → 1900s. A
// fixed pivot (not a clock-relative window) keeps the mapping deterministic and
// the tests hermetic; business documents cluster in the present decade, and the
// pivot only rots in 2069.
const dealDateCentury = 68

// excelEpoch is the base of the Excel 1900 serial system: serial 1 is
// 1900-01-01, and Excel's phantom 1900-02-29 makes 1899-12-30 the correct base
// for every serial ≥ 61. dealDateSerial{Min,Max} keep us well above that bug and
// away from stray numbers — the window is 1954-10 … 2064-04.
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

const (
	dealDateSerialMin = 20000
	dealDateSerialMax = 60000
)

var (
	// dealDateParen strips a trailing weekday/annotation — "2026-07-16 (목)".
	dealDateParen = regexp.MustCompile(`\s*[(（][^)）]*[)）]`)
	// dealDateSerialRE matches a bare Excel serial, optional fractional day.
	dealDateSerialRE = regexp.MustCompile(`^\d{5}(?:\.\d+)?$`)
	dealDateDigits   = regexp.MustCompile(`\d+`)
)

// normalizeDealDate converts a free-text 문서 일자 to YYYY-MM-DD. anchor dates
// the year-less forms ("7/8") and is the filing time — a document is filed on
// or within days of its own date, so the nearest candidate year is the right
// one. ok=false means the text carries no single resolvable date and the caller
// must keep it raw.
//
// Recognized: YYYY-MM-DD · YYYY-MM-DD (요일) · YY.MM.DD · YYYY.M.D ·
// YYYY. M. D. · YYYY년 M월 D일 · YYMMDD · YYYYMMDD · M/D · a range's start
// ("7/17~18") · Excel serial.
func normalizeDealDate(raw string, anchor time.Time) (string, bool) {
	iso, _, ok := normalizeDealDateDetail(raw, anchor)
	return iso, ok
}

// normalizeDealDateDetail is normalizeDealDate plus whether the year had to be
// inferred from the anchor rather than read from the text ("7/8"). The backfill
// reports those rows separately: every other conversion is a re-spelling, that
// one is a judgement the operator may want to eyeball.
func normalizeDealDateDetail(raw string, anchor time.Time) (iso string, yearInferred bool, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false, false
	}
	// An unfilled contract blank ("2026년 [*]월 [*]일", "2026. [ ]. [ ].") has
	// digits but no date: reject before the digit groups look convincing.
	if strings.ContainsAny(s, "[]") {
		return "", false, false
	}
	// A range names one document date at its start: "7/17~18", "26.08.10~12".
	if i := strings.IndexAny(s, "~∼〜"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.TrimSpace(dealDateParen.ReplaceAllString(s, ""))
	if s == "" {
		return "", false, false
	}
	if dealDateSerialRE.MatchString(s) {
		iso, ok := excelSerialDate(s)
		return iso, false, ok
	}

	groups := dealDateDigits.FindAllString(s, -1)
	switch len(groups) {
	case 1:
		iso, ok := compactDate(groups[0])
		return iso, false, ok
	case 2:
		// Either a month-only form ("2026-07", "2026. 8.", "2025.04") — no
		// single day, so raw — or a year-less M/D the anchor resolves.
		if len(groups[0]) == 4 {
			return "", false, false
		}
		iso, ok := anchoredDate(atoi(groups[0]), atoi(groups[1]), anchor)
		return iso, ok, ok
	case 3:
		y, m, d := atoi(groups[0]), atoi(groups[1]), atoi(groups[2])
		switch len(groups[0]) {
		case 4: // already a full year
		case 2:
			y = expandYear(y)
		default:
			// A one-digit lead ("1/2/3") is not a year in any Korean
			// convention — reading it as one would invent a date rather than
			// re-spell one. Ranges like "7/17~18" never reach here; they are
			// split at the "~" above.
			return "", false, false
		}
		iso, ok := isoOf(y, m, d)
		return iso, false, ok
	default:
		// Four or more groups is not one date — "2026-08-24, 2026-09-14".
		return "", false, false
	}
}

// compactDate reads an unseparated date: YYMMDD ("260713") or YYYYMMDD. A bare
// year or any other length carries no day.
func compactDate(s string) (string, bool) {
	switch len(s) {
	case 6:
		return isoOf(expandYear(atoi(s[:2])), atoi(s[2:4]), atoi(s[4:6]))
	case 8:
		return isoOf(atoi(s[:4]), atoi(s[4:6]), atoi(s[6:8]))
	default:
		return "", false
	}
}

// anchoredDate resolves a year-less M/D against the filing time, picking the
// candidate year nearest the anchor. That reads a 12/30 filed on 01-02 as last
// December and a 1/2 filed on 12-30 as next January, which is how the year-less
// convention is actually used — the alternative year is ~360 days away, never a
// close call.
func anchoredDate(m, d int, anchor time.Time) (string, bool) {
	if anchor.IsZero() {
		return "", false
	}
	best, bestGap, found := "", time.Duration(0), false
	for _, y := range []int{anchor.Year() - 1, anchor.Year(), anchor.Year() + 1} {
		iso, ok := isoOf(y, m, d)
		if !ok {
			continue // 2/29 exists only in the leap candidate
		}
		gap := time.Date(y, time.Month(m), d, 0, 0, 0, 0, anchor.Location()).Sub(anchor)
		if gap < 0 {
			gap = -gap
		}
		if !found || gap < bestGap {
			best, bestGap, found = iso, gap, true
		}
	}
	return best, found
}

// excelSerialDate converts an Excel 1900-system serial to ISO. The fractional
// part is the time of day and is dropped; out-of-window serials are refused so
// an unrelated five-digit number never becomes a date.
func excelSerialDate(s string) (string, bool) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < dealDateSerialMin || v > dealDateSerialMax {
		return "", false
	}
	return excelEpoch.AddDate(0, 0, int(v)).Format("2006-01-02"), true
}

// expandYear applies the fixed century pivot to a two-digit year; a year
// already written in full passes through.
func expandYear(y int) int {
	if y >= 100 {
		return y
	}
	if y <= dealDateCentury {
		return 2000 + y
	}
	return 1900 + y
}

// isoOf validates a Y/M/D triple by round-tripping through time.Date, which
// silently rolls impossible dates over ("2026-02-31" → 2026-03-03) — comparing
// the result back is what rejects them.
func isoOf(y, m, d int) (string, bool) {
	if y < 1900 || y > 2100 || m < 1 || m > 12 || d < 1 || d > 31 {
		return "", false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != m || t.Day() != d {
		return "", false
	}
	return t.Format("2006-01-02"), true
}

func atoi(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return v
}

// dealDateImplausibleDays is how far a 문서 일자 may sit from its filing time
// before the row is worth a second look. Measured against the production ledger
// (145 rows, 2026-09-06): the median gap is 0 days and genuinely late-filed
// contracts reach 105, while the three rows found carrying a wrong year sat at
// 365, 736 and 762. 270 clears the real tail with margin and still catches a
// one-year slip.
const dealDateImplausibleDays = 270

// warnImplausibleDealDate flags a filed document whose date is implausibly far
// from its filing time — the signature of extraction reading the wrong year off
// the source (observed: 2025 for 2026, 2024 for 2026). Nothing downstream can
// notice on its own, because a wrong-but-valid ISO date sums and buckets exactly
// as confidently as a right one; the 2025 slip had already caused a live
// deadline to be auto-released as "345일 경과".
//
// It warns and never rejects: filing a genuinely old document happens, and which
// one this is takes the source document, not a threshold.
func warnImplausibleDealDate(rec DealRecord, now time.Time) {
	if !isoDate(rec.Date) {
		return // unresolved dates are already visible as raw text
	}
	// Both sides must be midnight in the same location: parsing the date as UTC
	// and subtracting a local now shifts the boundary by the zone offset (KST
	// turned a 271-day gap into 270 and silenced it). Round rather than
	// truncate so a DST-shifted day is still a whole day.
	d, err := time.ParseInLocation("2006-01-02", rec.Date, now.Location())
	if err != nil {
		return
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	days := int(math.Round(today.Sub(d).Hours() / 24))
	if days < 0 {
		days = -days
	}
	if days <= dealDateImplausibleDays {
		return
	}
	slog.Warn("wiki: filed deal document date is far from its filing time; check the source year",
		"counterparty", rec.Counterparty,
		"docType", rec.DocType,
		"date", rec.Date,
		"dateRaw", rec.DateRaw,
		"filedOn", now.Format("2006-01-02"),
		"gapDays", days,
		"sourceRef", rec.SourceRef)
}
