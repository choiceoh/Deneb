package memory

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestCategoryImportanceMultiplier(t *testing.T) {
	// Decision facts should get boosted importance, context should be attenuated.
	decisionImp := math.Min(1.0, 0.8*categoryImportanceMultiplier[CategoryDecision])
	contextImp := math.Min(1.0, 0.8*categoryImportanceMultiplier[CategoryContext])

	if decisionImp <= contextImp {
		t.Errorf("decision adjusted importance (%.3f) should exceed context (%.3f)", decisionImp, contextImp)
	}

	// user_model should outweigh solution at equal raw importance.
	userModelImp := math.Min(1.0, 0.7*categoryImportanceMultiplier[CategoryUserModel])
	solutionImp := math.Min(1.0, 0.7*categoryImportanceMultiplier[CategorySolution])

	if userModelImp <= solutionImp {
		t.Errorf("user_model adjusted importance (%.3f) should exceed solution (%.3f)", userModelImp, solutionImp)
	}
}

func TestCategoryImportanceClamp(t *testing.T) {
	// Even with the 1.2x decision boost, importance should never exceed 1.0.
	adjusted := math.Min(1.0, 0.95*categoryImportanceMultiplier[CategoryDecision])
	if adjusted > 1.0 {
		t.Errorf("adjusted importance should be clamped to 1.0, got %f", adjusted)
	}
}

func TestCategoryHalfLife(t *testing.T) {
	// Context should decay much faster than user_model.
	contextHL := categoryHalfLifeDays[CategoryContext]
	userModelHL := categoryHalfLifeDays[CategoryUserModel]

	if contextHL >= userModelHL {
		t.Errorf("context half-life (%.0f) should be shorter than user_model (%.0f)", contextHL, userModelHL)
	}

	// After 30 days, context recency score should be much lower than decision.
	days := 30.0
	contextRecency := math.Exp(-math.Ln2 * days / contextHL)
	decisionRecency := math.Exp(-math.Ln2 * days / categoryHalfLifeDays[CategoryDecision])

	if contextRecency >= decisionRecency {
		t.Errorf("at 30 days, context recency (%.3f) should be lower than decision (%.3f)", contextRecency, decisionRecency)
	}

	// Context at 14 days should be ~0.5 (its half-life).
	contextAt14 := math.Exp(-math.Ln2 * 14.0 / contextHL)
	if math.Abs(contextAt14-0.5) > 0.01 {
		t.Errorf("context recency at 14 days should be ~0.5, got %.3f", contextAt14)
	}
}

func TestFrequencyScoring(t *testing.T) {
	// Simulate frequency scoring with different access counts.
	maxAccess := 100
	logMax := math.Log2(1 + float64(maxAccess))

	score0 := math.Log2(1+float64(0)) / logMax
	score10 := math.Log2(1+float64(10)) / logMax
	score100 := math.Log2(1+float64(100)) / logMax

	// Zero access should score 0.
	if score0 != 0 {
		t.Errorf("zero access should score 0, got %f", score0)
	}

	// Higher access should score higher.
	if score10 <= score0 {
		t.Errorf("10 accesses (%.3f) should score higher than 0 (%.3f)", score10, score0)
	}
	if score100 <= score10 {
		t.Errorf("100 accesses (%.3f) should score higher than 10 (%.3f)", score100, score10)
	}

	// Max access should score 1.0.
	if math.Abs(score100-1.0) > 0.001 {
		t.Errorf("max access should score 1.0, got %f", score100)
	}

	// Logarithmic scaling: 10 accesses should be well above 10% of max.
	if score10 < 0.4 {
		t.Errorf("log scaling: 10/100 accesses should score > 0.4, got %.3f", score10)
	}
}

func TestMergeAndRankWithNewWeights(t *testing.T) {
	s := tempStore(t)
	ctx := context.Background()

	now := time.Now()
	past30 := now.Add(-30 * 24 * time.Hour)

	// Insert a decision fact (high importance, some accesses).
	idDec, _ := s.InsertFact(ctx, Fact{
		Content:    "항상 gofmt 실행 후 커밋",
		Category:   CategoryDecision,
		Importance: 0.9,
	})
	// Simulate accesses.
	for range 5 {
		s.GetFact(ctx, idDec)
	}

	// Insert a context fact (medium importance, no accesses, old).
	idCtx, _ := s.InsertFact(ctx, Fact{
		Content:    "현재 리팩토링 진행 중",
		Category:   CategoryContext,
		Importance: 0.6,
	})
	// Make context fact appear old by updating created_at.
	s.db.Exec(`UPDATE facts SET created_at = ? WHERE id = ?`, past30.Format(time.RFC3339), idCtx)

	// Build fake search scores (equal hybrid scores).
	ftsResults := map[int64]float64{idDec: 0.7, idCtx: 0.7}
	vecResults := map[int64]float64{idDec: 0.7, idCtx: 0.7}

	results := s.mergeAndRank(ftsResults, vecResults, SearchOpts{Limit: 10})

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Decision fact should rank higher due to:
	// - Higher adjusted importance (0.9 × 1.2 → 1.0 vs 0.6 × 0.8 → 0.48)
	// - Better recency (fresh vs 30 days old with 14-day half-life)
	// - Higher frequency (5 accesses vs 0)
	if results[0].Fact.ID != idDec {
		t.Errorf("decision fact should rank first, got fact ID %d", results[0].Fact.ID)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("decision score (%.3f) should exceed context score (%.3f)",
			results[0].Score, results[1].Score)
	}
}

func TestWeightsSumToOne(t *testing.T) {
	sum := weightHybrid + weightImportance + weightRecency + weightFrequency
	if math.Abs(sum-1.0) > 0.001 {
		t.Errorf("weights should sum to 1.0, got %f", sum)
	}
}
