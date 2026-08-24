package wiki

import (
	"testing"
	"time"
)

// The factor curve is the whole contract: neutral below the evidence bar,
// ramping to the floor, and any observed use is full rehabilitation.
func TestTRSFactorCurve(t *testing.T) {
	cases := []struct {
		name string
		u    RecallUsage
		want float64
	}{
		{"below evidence bar stays neutral", RecallUsage{Injects: trsMinExposures - 1}, 1},
		{"at the bar demotion starts at zero", RecallUsage{Injects: trsMinExposures}, 1},
		{"saturation hits the floor", RecallUsage{Injects: trsSaturationExposures}, trsFloor},
		{"beyond saturation stays at the floor", RecallUsage{Injects: 100}, trsFloor},
		{"one read rehabilitates fully", RecallUsage{Injects: 100, Reads: 1}, 1},
		{"one cite rehabilitates fully", RecallUsage{Injects: 100, Cites: 1}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trsFactorFor(tc.u); got != tc.want {
				t.Errorf("trsFactorFor(%+v) = %v, want %v", tc.u, got, tc.want)
			}
		})
	}
	mid := trsFactorFor(RecallUsage{Injects: (trsMinExposures + trsSaturationExposures) / 2})
	if mid <= trsFloor || mid >= 1 {
		t.Errorf("midpoint factor %v not strictly between floor and 1", mid)
	}
	// The floor must sit above the archived demotion — an ignored page is
	// still a current page (see validityFactor's ladder).
	if trsFloor <= 0.3 {
		t.Errorf("trsFloor %v must stay above the archived factor 0.3", trsFloor)
	}
}

func trsInjectN(t *testing.T, store *Store, path string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := store.RecordRecallEvents(injectEvents(path)); err != nil {
			t.Fatalf("record inject: %v", err)
		}
	}
}

// End to end through the ledger: a chronically ignored page drops below an
// equally scored page, and a used page is untouched.
func TestApplyRecallTRSDemotesIgnoredPageOnly(t *testing.T) {
	store := newRecallHitsStore(t)
	trsInjectN(t, store, "프로젝트/ignored.md", trsSaturationExposures)
	trsInjectN(t, store, "프로젝트/used.md", trsSaturationExposures)
	if err := store.RecordRecallEvents([]RecallEvent{{Path: "프로젝트/used.md", Event: RecallEventCite}}); err != nil {
		t.Fatalf("record cite: %v", err)
	}

	results := []SearchResult{
		{Path: "프로젝트/ignored.md", Score: 10},
		{Path: "프로젝트/used.md", Score: 9},
		{Path: "프로젝트/fresh.md", Score: 8},
	}
	got := store.applyRecallTRS(results)
	if got[0].Path != "프로젝트/used.md" {
		t.Fatalf("used page should now lead: %+v", got)
	}
	if got[1].Path != "프로젝트/fresh.md" || got[2].Path != "프로젝트/ignored.md" {
		t.Errorf("ignored page should fall to the bottom: %+v", got)
	}
	if got[2].Score != 10*trsFloor {
		t.Errorf("ignored score = %v, want %v", got[2].Score, 10*trsFloor)
	}
	if got[0].Score != 9 || got[1].Score != 8 {
		t.Errorf("non-demoted scores must be untouched: %+v", got)
	}
}

// The kill switch must bypass the whole pass, and a store with no qualifying
// evidence must return the slice unchanged (no re-sort, no allocation churn).
func TestApplyRecallTRSKillSwitchAndNoEvidence(t *testing.T) {
	store := newRecallHitsStore(t)
	trsInjectN(t, store, "프로젝트/ignored.md", trsSaturationExposures)
	results := []SearchResult{{Path: "프로젝트/ignored.md", Score: 10}}

	t.Setenv("DENEB_WIKI_TRS", "0")
	if got := store.applyRecallTRS(results); got[0].Score != 10 {
		t.Errorf("kill switch ignored: %+v", got)
	}
	t.Setenv("DENEB_WIKI_TRS", "")

	clean := newRecallHitsStore(t)
	if got := clean.applyRecallTRS(results); got[0].Score != 10 {
		t.Errorf("empty ledger demoted: %+v", got)
	}
}

// The factor cache must refresh after its TTL so a page used since the last
// snapshot recovers without a restart.
func TestRecallTRSFactorsRefreshAfterTTL(t *testing.T) {
	store := newRecallHitsStore(t)
	trsInjectN(t, store, "프로젝트/p.md", trsSaturationExposures)

	now := time.Now()
	if f := store.recallTRSFactors(now)["프로젝트/p.md"]; f != trsFloor {
		t.Fatalf("initial factor = %v, want %v", f, trsFloor)
	}
	if err := store.RecordRecallEvents([]RecallEvent{{Path: "프로젝트/p.md", Event: RecallEventRead}}); err != nil {
		t.Fatalf("record read: %v", err)
	}
	// Within the TTL the stale factor persists (deliberate — bounded I/O)...
	if _, ok := store.recallTRSFactors(now.Add(time.Minute))["프로젝트/p.md"]; !ok {
		t.Fatal("factor evaporated inside the TTL window")
	}
	// ...and after the TTL the read rehabilitates the page.
	if _, ok := store.recallTRSFactors(now.Add(trsRefreshInterval + time.Second))["프로젝트/p.md"]; ok {
		t.Error("used page still demoted after refresh")
	}
}
