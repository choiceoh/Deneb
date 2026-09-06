package wiki

import (
	"testing"
	"time"
)

// anchorAt is the filing time these cases are dated against. Year-less forms
// resolve relative to it; every other case must ignore it entirely.
func anchorAt(t *testing.T, iso string) time.Time {
	t.Helper()
	tm, err := time.ParseInLocation("2006-01-02", iso, time.Local)
	if err != nil {
		t.Fatalf("bad anchor %q: %v", iso, err)
	}
	return tm
}

// TestNormalizeDealDateProductionForms walks every distinct 문서 일자 shape the
// production ledger actually holds (145 rows, 2026-09-06), so the parser is
// pinned to observed data rather than to imagined formats.
func TestNormalizeDealDateProductionForms(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		anchor string // filing time
		want   string // "" = must stay raw
	}{
		// Already ISO — passes through untouched.
		{"iso", "2026-08-31", "2026-08-31", "2026-08-31"},

		// Dominant form: two-digit year, dot separated (~50 rows).
		{"yy dot", "26.08.31", "2026-08-31", "2026-08-31"},
		{"yy dot sep month", "26.09.04", "2026-09-04", "2026-09-04"},
		{"yy dot july", "26.07.15", "2026-07-15", "2026-07-15"},

		// ISO with a Korean weekday annotation.
		{"iso weekday", "2026-07-16 (목)", "2026-07-16", "2026-07-16"},
		{"iso weekday wed", "2026-07-15 (수)", "2026-07-16", "2026-07-15"},
		{"iso weekday fullwidth", "2026-07-16（목）", "2026-07-16", "2026-07-16"},

		// Four-digit year, dots, with and without the spacing/trailing dot.
		{"yyyy spaced dots", "2026. 6. 30.", "2026-06-30", "2026-06-30"},
		{"yyyy tight dots", "2026.6.24", "2026-06-29", "2026-06-24"},
		{"yyyy tight dots 2", "2026.6.30", "2026-06-30", "2026-06-30"},

		// Korean 년/월/일, padded and unpadded.
		{"korean words", "2026년 6월 29일", "2026-07-01", "2026-06-29"},
		{"korean words padded", "2026년 07월 09일", "2026-07-09", "2026-07-09"},

		// Unseparated compact forms.
		{"yymmdd", "260713", "2026-07-13", "2026-07-13"},
		{"yyyymmdd", "20260713", "2026-07-13", "2026-07-13"},

		// Excel serial from a spreadsheet-sourced 견적서. The row was filed
		// 2026-08-20, which is what makes the conversion checkable.
		{"excel serial", "46254.543659143521", "2026-08-20", "2026-08-20"},
		{"excel serial whole", "46254", "2026-08-20", "2026-08-20"},

		// Year-less M/D — resolved against the filing time.
		{"slash same day", "7/8", "2026-07-08", "2026-07-08"},
		{"slash days later", "7/8", "2026-07-10", "2026-07-08"},
		{"slash june", "6/29", "2026-06-29", "2026-06-29"},
		{"slash single digit day", "7/3", "2026-07-01", "2026-07-03"},

		// A range names one document date: its start.
		{"slash range", "7/17~18", "2026-07-14", "2026-07-17"},
		{"dotted range", "26.08.10~12", "2026-08-12", "2026-08-10"},

		// Month-only: a real month but no day, so no ISO date to give.
		{"month only iso", "2026-07", "2026-07-13", ""},
		{"month only dots", "2026. 8.", "2026-08-28", ""},
		{"month only tight", "2025.04", "2026-06-29", ""},

		// Unfilled contract blanks.
		{"placeholder star", "2026년 [*]월 [*]일", "2026-07-10", ""},
		{"placeholder space", "2026. [ ]. [ ].", "2026-07-10", ""},

		// Relative deadlines (these reach the parser via DueDate).
		{"relative weeks", "PO 확정 후 약 3주", "2026-08-20", ""},
		{"relative days", "계약 후 7일 이내", "2026-08-20", ""},
		{"relative month end", "9월 말", "2026-08-20", ""},

		// Two dates on one field is not one date.
		{"two dates", "2026-08-24, 2026-09-14", "2026-08-20", ""},

		// Truncated / year-only.
		{"truncated", "2026-07-", "2026-07-20", ""},
		{"year only", "2026", "2026-07-20", ""},
		{"empty", "", "2026-07-20", ""},
		{"blank", "   ", "2026-07-20", ""},

		// Impossible calendar dates must be refused, not rolled over.
		{"impossible day", "2026-02-31", "2026-02-28", ""},
		{"impossible month", "26.13.01", "2026-08-01", ""},
		{"zero day", "2026.7.0", "2026-07-20", ""},
		// A one-digit lead is not a year: "1/2/3" must not become 2001-02-03.
		{"one digit lead triple", "1/2/3", "2026-07-20", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := normalizeDealDate(tc.raw, anchorAt(t, tc.anchor))
			if tc.want == "" {
				if ok {
					t.Fatalf("normalizeDealDate(%q) = %q, ok — want unparsed (must stay raw)", tc.raw, got)
				}
				return
			}
			if !ok {
				t.Fatalf("normalizeDealDate(%q) not parsed — want %q", tc.raw, tc.want)
			}
			if got != tc.want {
				t.Fatalf("normalizeDealDate(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestNormalizeDealDateCenturyPivot pins the two-digit-year rule: a fixed pivot
// at 68, not a window that drifts with the clock. Documents dated "99.03.01"
// are 1999 filings, not 2099 ones, and the mapping must not depend on when the
// test runs.
func TestNormalizeDealDateCenturyPivot(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"00.01.02", "2000-01-02"},
		{"26.08.31", "2026-08-31"},
		{"68.12.31", "2068-12-31"}, // last year on the 2000s side
		{"69.01.01", "1969-01-01"}, // first year on the 1900s side
		{"99.03.01", "1999-03-01"},
	}
	// Two anchors decades apart: the pivot must give the same answer for both.
	for _, anchor := range []string{"2026-08-31", "2005-01-01"} {
		for _, tc := range cases {
			got, ok := normalizeDealDate(tc.raw, anchorAt(t, anchor))
			if !ok || got != tc.want {
				t.Fatalf("anchor %s: normalizeDealDate(%q) = %q,%v — want %q", anchor, tc.raw, got, ok, tc.want)
			}
		}
	}
}

// TestNormalizeDealDateAnchorYearBoundary covers the only case where the
// year-less M/D inference is not obvious: a filing that crosses New Year. The
// nearest candidate year wins, in both directions.
func TestNormalizeDealDateAnchorYearBoundary(t *testing.T) {
	cases := []struct{ raw, anchor, want string }{
		{"12/30", "2027-01-02", "2026-12-30"}, // filed days after, last year
		{"1/2", "2026-12-30", "2027-01-02"},   // dated days ahead, next year
		{"12/30", "2026-12-30", "2026-12-30"},
		{"2/29", "2028-03-01", "2028-02-29"}, // only the leap candidate exists
	}
	for _, tc := range cases {
		got, ok := normalizeDealDate(tc.raw, anchorAt(t, tc.anchor))
		if !ok || got != tc.want {
			t.Fatalf("normalizeDealDate(%q, %s) = %q,%v — want %q", tc.raw, tc.anchor, got, ok, tc.want)
		}
	}
}

// TestNormalizeDealDateSerialWindow keeps the Excel-serial branch from
// swallowing unrelated five-digit numbers: only the 1954–2064 window converts.
func TestNormalizeDealDateSerialWindow(t *testing.T) {
	for _, raw := range []string{"19999", "60001", "12345"} {
		if got, ok := normalizeDealDate(raw, anchorAt(t, "2026-08-20")); ok {
			t.Fatalf("normalizeDealDate(%q) = %q — a serial outside the window must stay raw", raw, got)
		}
	}
	// The window's own edges do convert, and land where Excel puts them.
	for _, tc := range []struct{ raw, want string }{
		{"20000", "1954-10-03"},
		{"60000", "2064-04-08"},
	} {
		got, ok := normalizeDealDate(tc.raw, anchorAt(t, "2026-08-20"))
		if !ok || got != tc.want {
			t.Fatalf("normalizeDealDate(%q) = %q,%v — want %q", tc.raw, got, ok, tc.want)
		}
	}
}

// TestDealRecordFromNormalizesDates checks the filing path itself: Date lands
// ISO, the source text survives in DateRaw, and an already-ISO or unresolvable
// date leaves DateRaw empty (so existing rows keep their exact shape).
func TestDealRecordFromNormalizesDates(t *testing.T) {
	now := anchorAt(t, "2026-08-31")

	rec := dealRecordFrom(DealPageInput{Counterparty: "연우철강", Date: "26.08.26", DueDate: "2026.9.14"}, now)
	if rec.Date != "2026-08-26" {
		t.Fatalf("Date = %q, want 2026-08-26", rec.Date)
	}
	if rec.DateRaw != "26.08.26" {
		t.Fatalf("DateRaw = %q, want the source text 26.08.26", rec.DateRaw)
	}
	if rec.DueDate != "2026-09-14" || rec.DueDateRaw != "2026.9.14" {
		t.Fatalf("DueDate = %q / raw %q, want 2026-09-14 / 2026.9.14", rec.DueDate, rec.DueDateRaw)
	}

	// Already ISO: no raw recorded, nothing rewritten.
	iso := dealRecordFrom(DealPageInput{Counterparty: "트라이브", Date: "2026-06-10"}, now)
	if iso.Date != "2026-06-10" || iso.DateRaw != "" {
		t.Fatalf("ISO input: Date = %q, DateRaw = %q — want unchanged and no raw", iso.Date, iso.DateRaw)
	}

	// Unresolvable: the original stays on Date, exactly as before this parser.
	tmpl := dealRecordFrom(DealPageInput{Counterparty: "해봄에너지", Date: "2026년 [*]월 [*]일"}, now)
	if tmpl.Date != "2026년 [*]월 [*]일" || tmpl.DateRaw != "" {
		t.Fatalf("placeholder: Date = %q, DateRaw = %q — want the raw text kept on Date", tmpl.Date, tmpl.DateRaw)
	}

	// A relative deadline is not a date and must not be invented.
	rel := dealRecordFrom(DealPageInput{Counterparty: "다스코", DueDate: "계약 후 7일 이내"}, now)
	if rel.DueDate != "계약 후 7일 이내" || rel.DueDateRaw != "" {
		t.Fatalf("relative due: DueDate = %q, raw = %q — want kept verbatim", rel.DueDate, rel.DueDateRaw)
	}

	// No date at all still falls back to the filing day.
	empty := dealRecordFrom(DealPageInput{Counterparty: "무일자"}, now)
	if empty.Date != "2026-08-31" || empty.DateRaw != "" {
		t.Fatalf("empty date: Date = %q, DateRaw = %q — want the filing day", empty.Date, empty.DateRaw)
	}
}
