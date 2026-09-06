package wiki

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// backfillStore writes lines verbatim into a fresh store's ledger, so the tests
// can pin byte-level behavior against rows shaped exactly like production's.
func backfillStore(t *testing.T, lines ...string) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	path := filepath.Join(s.dir, dealRecordsFile)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	return s, path
}

// recordedAt 1787184000000 = 2026-08-20 00:00:00 UTC — the filing time these
// rows are anchored to.
const backfillAnchorMillis = `1787184000000`

func TestBackfillDealDatesRewritesAndPreservesEverythingElse(t *testing.T) {
	// Field order, the unparsed-amount flag, a nested terms object and a float
	// amount are all here so the rewrite has something to corrupt if it decodes
	// and re-encodes the row instead of editing it.
	row := `{"counterparty":"연우철강","docType":"발주품의","amountRaw":"1,234,567원","amountValue":1234567,"currency":"KRW","amountParsed":true,"date":"26.08.26","dueDate":"2026.9.14","items":["앵글"],"summary":"자재 발주","sourceRef":"mail:x1","terms":{"capacityMW":1.5},"recordedAt":` + backfillAnchorMillis + `}`
	isoRow := `{"counterparty":"트라이브","date":"2026-06-10","amountValue":800000.5,"recordedAt":` + backfillAnchorMillis + `}`
	s, path := backfillStore(t, row, isoRow)

	// Dry run must not touch the file.
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rep, err := s.BackfillDealDates(false)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("dry run rewrote the ledger")
	}
	if len(rep.Changed) != 2 || rep.RowsWritten != 1 || rep.AlreadyISO != 1 || rep.Rows != 2 {
		t.Fatalf("dry-run report = %+v", rep)
	}
	if rep.BackupPath != "" {
		t.Errorf("dry run wrote a backup at %q", rep.BackupPath)
	}

	// Apply.
	rep, err = s.BackfillDealDates(true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rep.BackupPath == "" {
		t.Fatal("apply wrote no backup")
	}
	if bak, err := os.ReadFile(rep.BackupPath); err != nil || string(bak) != string(before) {
		t.Fatalf("backup does not hold the pre-run ledger (err=%v)", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after apply: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("line count = %d, want 2", len(lines))
	}
	// The already-ISO row must come out byte-identical — including its float,
	// which a re-encode could turn into 8.000005e+05.
	if lines[1] != isoRow {
		t.Errorf("ISO row was rewritten:\n got %s\nwant %s", lines[1], isoRow)
	}
	// The rewritten row differs only in its date fields, and keeps key order
	// with each raw inserted right after the field it came from.
	want := `{"counterparty":"연우철강","docType":"발주품의","amountRaw":"1,234,567원","amountValue":1234567,"currency":"KRW","amountParsed":true,"date":"2026-08-26","dateRaw":"26.08.26","dueDate":"2026-09-14","dueDateRaw":"2026.9.14","items":["앵글"],"summary":"자재 발주","sourceRef":"mail:x1","terms":{"capacityMW":1.5},"recordedAt":` + backfillAnchorMillis + `}`
	if lines[0] != want {
		t.Errorf("rewritten row:\n got %s\nwant %s", lines[0], want)
	}

	// The rewrite must survive the reader it exists for.
	recs, err := s.ListDealRecords()
	if err != nil || len(recs) != 2 {
		t.Fatalf("ListDealRecords = %d rows, err %v", len(recs), err)
	}
	if recs[0].Date != "2026-08-26" || recs[0].DateRaw != "26.08.26" {
		t.Errorf("row 0 = %q / raw %q", recs[0].Date, recs[0].DateRaw)
	}
	if recs[0].AmountValue != 1234567 || !recs[0].AmountParsed || recs[0].Terms == nil {
		t.Errorf("non-date fields damaged: %+v", recs[0])
	}
}

func TestBackfillDealDatesIsIdempotent(t *testing.T) {
	row := `{"counterparty":"화웨이","date":"26.08.19","recordedAt":` + backfillAnchorMillis + `}`
	s, path := backfillStore(t, row)

	if _, err := s.BackfillDealDates(true); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first, _ := os.ReadFile(path)

	rep, err := s.BackfillDealDates(true)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("second run changed the ledger:\n first %s\nsecond %s", first, second)
	}
	if rep.RowsWritten != 0 || len(rep.Changed) != 0 {
		t.Errorf("second run reported work: %+v", rep)
	}
	if rep.BackupPath != "" {
		t.Errorf("second run wrote a backup though nothing changed: %s", rep.BackupPath)
	}
	// The first run's raw is still there — a re-run must not overwrite the
	// source text with the ISO value it already produced.
	var rec DealRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(second))), &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.DateRaw != "26.08.19" {
		t.Errorf("DateRaw = %q, want the original 26.08.19", rec.DateRaw)
	}
}

func TestBackfillDealDatesKeepsUnresolvableAndMalformedRows(t *testing.T) {
	tmpl := `{"counterparty":"해봄에너지","date":"2026년 [*]월 [*]일","recordedAt":` + backfillAnchorMillis + `}`
	monthOnly := `{"counterparty":"AIKO","date":"2026-07","recordedAt":` + backfillAnchorMillis + `}`
	junk := `{not json`
	s, path := backfillStore(t, tmpl, monthOnly, junk)

	rep, err := s.BackfillDealDates(true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(rep.Changed) != 0 || rep.RowsWritten != 0 {
		t.Fatalf("report = %+v, want nothing rewritten", rep)
	}
	if len(rep.Unresolved) != 2 {
		t.Errorf("Unresolved = %+v, want the placeholder and the month-only row", rep.Unresolved)
	}
	got, _ := os.ReadFile(path)
	// Every line survives, including the one we cannot parse — a migration that
	// drops rows it does not understand is worse than one that skips them.
	for _, want := range []string{tmpl, monthOnly, junk} {
		if !strings.Contains(string(got), want) {
			t.Errorf("line lost from the ledger: %s", want)
		}
	}
}

func TestBackfillDealDatesAnchorsYearlessDatesToFilingTime(t *testing.T) {
	// 7/8 filed 2026-08-20 → 2026-07-08, and the run flags it as inferred.
	row := `{"counterparty":"라이젠코리아","date":"7/8","recordedAt":` + backfillAnchorMillis + `}`
	// A row with no filing time has no anchor, so a year-less date stays raw
	// rather than being resolved against the wall clock.
	noAnchor := `{"counterparty":"미상","date":"7/8"}`
	s, _ := backfillStore(t, row, noAnchor)

	rep, err := s.BackfillDealDates(false)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(rep.Changed) != 1 {
		t.Fatalf("Changed = %+v, want only the anchored row", rep.Changed)
	}
	c := rep.Changed[0]
	if c.To != "2026-07-08" || !c.YearInferred {
		t.Errorf("change = %+v, want 2026-07-08 flagged as inferred", c)
	}
	if len(rep.Unresolved) != 1 || rep.Unresolved[0].Counterparty != "미상" {
		t.Errorf("Unresolved = %+v, want the row with no filing time", rep.Unresolved)
	}
}

func TestBackfillDealDatesRefusesConcurrentAppend(t *testing.T) {
	row := `{"counterparty":"화웨이","date":"26.08.19","recordedAt":` + backfillAnchorMillis + `}`
	s, path := backfillStore(t, row)

	// Stand in for the production gateway filing a document mid-migration: the
	// run must abort rather than replace the file and lose the appended row.
	appended := `{"counterparty":"신규","date":"2026-09-05","recordedAt":` + backfillAnchorMillis + `}`
	orig, _ := os.ReadFile(path)
	dealDateBackfillPreWriteHook = func() {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatalf("simulate append: %v", err)
		}
		defer f.Close()
		if _, err := f.WriteString(appended + "\n"); err != nil {
			t.Fatalf("simulate append: %v", err)
		}
	}
	defer func() { dealDateBackfillPreWriteHook = nil }()
	if _, err := s.BackfillDealDates(true); err == nil {
		t.Fatal("apply succeeded despite a concurrent append")
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), appended) {
		t.Error("the concurrently appended row was lost")
	}
	if !strings.Contains(string(got), string(bytes.TrimSpace(orig))) {
		t.Error("the original row was rewritten despite the abort")
	}
}
