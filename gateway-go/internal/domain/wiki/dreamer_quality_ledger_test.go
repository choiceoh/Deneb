package wiki

import (
	"os"
	"strings"
	"testing"
	"time"
)

func newQualityDreamer(t *testing.T) *WikiDreamer {
	t.Helper()
	s := missStore(t)
	return &WikiDreamer{store: s}
}

// The quality ledger must round-trip entries, skip malformed lines, and keep a
// zero utility (all pages cold) distinct from a missing utility axis via the
// explicit UtilitySet flag.
func TestDreamQualityLedger_RoundTripAndMalformedSkip(t *testing.T) {
	wd := newQualityDreamer(t)
	if err := wd.appendDreamQualityEntry(dreamQualityEntry{Ts: 1, Score: 80, Utility: 0.5, UtilitySet: true}); err != nil {
		t.Fatal(err)
	}
	if err := wd.appendDreamQualityEntry(dreamQualityEntry{Ts: 2, Score: 0, Utility: 0.0, UtilitySet: true}); err != nil {
		t.Fatal(err)
	}
	// Inject a malformed line between valid ones.
	f, err := os.OpenFile(wd.dreamQualityPath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{broken json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := wd.appendDreamQualityEntry(dreamQualityEntry{Ts: 3, Score: 90}); err != nil {
		t.Fatal(err)
	}

	entries := wd.readDreamQualityEntries()
	if len(entries) != 3 {
		t.Fatalf("expected 3 valid entries (malformed skipped), got %d", len(entries))
	}
	if entries[0].Utility != 0.5 || !entries[0].UtilitySet {
		t.Errorf("entry 0 mangled: %+v", entries[0])
	}
	if entries[1].Utility != 0 || !entries[1].UtilitySet {
		t.Errorf("zero utility must round-trip as a set axis: %+v", entries[1])
	}
	if entries[2].Score != 90 || entries[2].UtilitySet {
		t.Errorf("entry 2 mangled: %+v", entries[2])
	}
}

func TestRotateDreamQualityLedger_KeepsTail(t *testing.T) {
	wd := newQualityDreamer(t)
	for i := 0; i < dreamQualityLedgerKeep+50; i++ {
		if err := wd.appendDreamQualityEntry(dreamQualityEntry{Ts: int64(i), Score: float64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	wd.rotateDreamQualityLedger(wd.dreamQualityPath())
	entries := wd.readDreamQualityEntries()
	if len(entries) != dreamQualityLedgerKeep {
		t.Fatalf("rotation should keep %d entries, got %d", dreamQualityLedgerKeep, len(entries))
	}
	if entries[0].Ts != 50 {
		t.Errorf("rotation must keep the TAIL (oldest kept ts=50), got %d", entries[0].Ts)
	}
}

func TestRenderDreamQualityTrend_RequiresTwoUtilityReadings(t *testing.T) {
	wd := newQualityDreamer(t)
	if got := wd.renderDreamQualityTrend(); got != "" {
		t.Fatalf("empty ledger must yield no trend, got %q", got)
	}
	// A scored cycle without a utility axis is not a trend input either.
	if err := wd.appendDreamQualityEntry(dreamQualityEntry{Ts: 1, Score: 70}); err != nil {
		t.Fatal(err)
	}
	if got := wd.renderDreamQualityTrend(); got != "" {
		t.Fatalf("no utility readings must yield no trend, got %q", got)
	}
}

func TestRenderDreamQualityTrend_RisingFallingAndFlat(t *testing.T) {
	rising := newQualityDreamer(t)
	for i, u := range []float64{0.2, 0.3, 0.5, 0.7} {
		if err := rising.appendDreamQualityEntry(dreamQualityEntry{Ts: int64(i), Utility: u, UtilitySet: true}); err != nil {
			t.Fatal(err)
		}
	}
	if got := rising.renderDreamQualityTrend(); !strings.Contains(got, "상승") {
		t.Errorf("rising trend not detected: %q", got)
	}

	falling := newQualityDreamer(t)
	for i, u := range []float64{0.7, 0.5, 0.3, 0.2} {
		if err := falling.appendDreamQualityEntry(dreamQualityEntry{Ts: int64(i), Utility: u, UtilitySet: true}); err != nil {
			t.Fatal(err)
		}
	}
	if got := falling.renderDreamQualityTrend(); !strings.Contains(got, "하락") {
		t.Errorf("falling trend not detected: %q", got)
	}

	flat := newQualityDreamer(t)
	for i, u := range []float64{0.50, 0.52, 0.49, 0.51} {
		if err := flat.appendDreamQualityEntry(dreamQualityEntry{Ts: int64(i), Utility: u, UtilitySet: true}); err != nil {
			t.Fatal(err)
		}
	}
	if got := flat.renderDreamQualityTrend(); !strings.Contains(got, "유지") {
		t.Errorf("flat trend not detected: %q", got)
	}
}

// A judgeable prior page that was never recalled must read utility 0 WITH the
// UtilitySet flag — the trend and ledger distinguish "0 (cold)" from "no axis".
func TestComputeDreamQuality_SetsUtilityFlag(t *testing.T) {
	now := time.Now()
	old := now.Add(-72 * time.Hour).Format(time.RFC3339)
	q := computeDreamQuality(dreamQualityInputs{
		proposed: 1,
		applied:  1,
		updates:  []wikiUpdate{{Confidence: "high"}},
		priorPaths: []processedDiaryCapsule{
			{At: old, Paths: []string{"프로젝트/a.md"}},
		},
		recalls: map[string]RecallUsage{},
		now:     now,
	})
	if !q.UtilitySet {
		t.Error("utility axis with a judgeable page must set UtilitySet")
	}
	if q.Utility != 0 {
		t.Errorf("unrecalled page should read utility 0, got %.2f", q.Utility)
	}

	q2 := computeDreamQuality(dreamQualityInputs{
		proposed: 1,
		applied:  1,
		updates:  []wikiUpdate{{Confidence: "high"}},
		now:      now,
	})
	if q2.UtilitySet {
		t.Error("no judgeable prior pages must leave UtilitySet false")
	}
}
