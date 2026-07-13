package wiki

import (
	"math"
	"testing"
	"time"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestComputeDreamQuality_AllThreeAxes(t *testing.T) {
	now := time.Now()
	old := now.Add(-72 * time.Hour).Format(time.RFC3339) // past the 48h grace
	in := dreamQualityInputs{
		proposed: 4,
		applied:  2, // precision 0.5
		updates: []wikiUpdate{
			{Confidence: "high"}, // 1.0
			{Confidence: "low"},  // 0.3 → mean 0.65
		},
		priorPaths: []processedDiaryCapsule{
			{At: old, Paths: []string{"프로젝트/a.md", "프로젝트/b.md"}}, // 1 of 2 recalled → utility 0.5
		},
		recalls: map[string]int{"프로젝트/a.md": 3},
		now:     now,
	}
	q := computeDreamQuality(in)
	if q.Signals != 3 {
		t.Fatalf("Signals = %d, want 3", q.Signals)
	}
	if !approx(q.Precision, 0.5) || !approx(q.Confidence, 0.65) || !approx(q.Utility, 0.5) {
		t.Fatalf("axes: precision=%.3f confidence=%.3f utility=%.3f", q.Precision, q.Confidence, q.Utility)
	}
	if q.RecalledPages != 1 {
		t.Errorf("RecalledPages = %d, want 1", q.RecalledPages)
	}
	// (0.5*0.4 + 0.65*0.2 + 0.5*0.4) / (0.4+0.2+0.4) * 100 = 53
	if want := (0.5*0.4 + 0.65*0.2 + 0.5*0.4) / 1.0 * 100; !approx(q.Score, want) {
		t.Errorf("Score = %.2f, want %.2f", q.Score, want)
	}
}

func TestComputeDreamQuality_MissingUtilityRenormalizes(t *testing.T) {
	// No prior capsules → utility axis has no evidence and must drop out; the
	// score comes from precision + confidence renormalized over their weights,
	// NOT diluted by a phantom zero-utility term.
	q := computeDreamQuality(dreamQualityInputs{
		proposed: 2,
		applied:  2, // precision 1.0
		updates:  []wikiUpdate{{Confidence: "high"}},
		now:      time.Now(),
	})
	if q.Signals != 2 {
		t.Fatalf("Signals = %d, want 2 (utility absent)", q.Signals)
	}
	// (1.0*0.4 + 1.0*0.2) / (0.4+0.2) * 100 = 100
	if !approx(q.Score, 100) {
		t.Errorf("Score = %.2f, want 100 (renormalized, not diluted)", q.Score)
	}
}

func TestComputeDreamQuality_FreshPagesExemptFromUtility(t *testing.T) {
	now := time.Now()
	fresh := now.Add(-1 * time.Hour).Format(time.RFC3339) // inside the 48h grace
	q := computeDreamQuality(dreamQualityInputs{
		proposed:   1,
		applied:    1,
		updates:    []wikiUpdate{{Confidence: "medium"}},
		priorPaths: []processedDiaryCapsule{{At: fresh, Paths: []string{"프로젝트/new.md"}}},
		recalls:    map[string]int{},
		now:        now,
	})
	// The fresh page must not enter the utility denominator, so utility is
	// unavailable (not 0) and only precision + confidence score.
	if q.Signals != 2 {
		t.Errorf("Signals = %d, want 2 (fresh page exempt, utility absent)", q.Signals)
	}
	if q.Utility != 0 || q.RecalledPages != 0 {
		t.Errorf("fresh page leaked into utility: utility=%.2f recalled=%d", q.Utility, q.RecalledPages)
	}
}

func TestComputeDreamQuality_IdleCycleUnscored(t *testing.T) {
	q := computeDreamQuality(dreamQualityInputs{now: time.Now()})
	if q.Signals != 0 || q.Score != 0 {
		t.Errorf("idle cycle should be unscored: signals=%d score=%.2f", q.Signals, q.Score)
	}
}
