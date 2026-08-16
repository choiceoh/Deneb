package genesis

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// Flip gate (RSI P1.5, AgentDevel 2601.04620): a candidate that regresses any
// previously-passing held-out case is rejected even when aggregate score
// improves — compensated regressions must not buy promotion.
func TestValidateCandidateFlipGateRejectsRegressionOfPreviouslyPassingCase(t *testing.T) {
	// The pool split hashes the dedupe identity (2/3 blind) — the gate only
	// scores blind cases, so pin every fixture into the blind pool by
	// deterministically probing ID suffixes.
	blind := func(t *testing.T, tc SkillValidationCaseRecord) SkillValidationCaseRecord {
		t.Helper()
		base := tc.ID
		for i := 0; i < 64; i++ {
			tc.ID = fmt.Sprintf("%s-%d", base, i)
			if validationCaseBlindHeldOut(tc) {
				return tc
			}
		}
		t.Fatalf("no blind ID suffix found for %s", base)
		return tc
	}
	engine := func(t *testing.T, cases ...SkillValidationCaseRecord) *SkillValidationEngine {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		tr, err := NewTracker(slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range cases {
			if err := tr.RecordSkillValidationCase(tc); err != nil {
				t.Fatal(err)
			}
		}
		return NewSkillValidationEngine(tr, slog.Default())
	}

	t.Run("compensated regression is rejected despite aggregate gain", func(t *testing.T) {
		v := engine(
			t,
			blind(t, SkillValidationCaseRecord{ID: "case-alpha", SkillName: "sk", RequiredSubstrings: []string{"alpha rule"}}),
			blind(t, SkillValidationCaseRecord{ID: "case-beta", SkillName: "sk", RequiredSubstrings: []string{"beta rule", "beta detail"}}),
		)
		// Original passes alpha (1/3); candidate drops alpha but gains both
		// beta assertions (2/3) — aggregate improves, old case regresses.
		res, err := v.ValidateCandidate("sk", "body with alpha rule", "body with beta rule and beta detail")
		if err != nil {
			t.Fatal(err)
		}
		if res.Pass {
			t.Fatalf("compensated regression must be rejected: %+v", res)
		}
		if !strings.Contains(res.Reason, "flip gate rejected") {
			t.Fatalf("rejection must come from the flip gate, got: %s", res.Reason)
		}
		if len(res.FlippedCases) != 1 || !strings.HasPrefix(res.FlippedCases[0], "case-alpha") {
			t.Fatalf("flipped cases = %v, want [case-alpha-*]", res.FlippedCases)
		}
	})

	t.Run("assertion churn inside an already-failing case is not a flip", func(t *testing.T) {
		v := engine(
			t,
			// Original passes only 1 of 2 assertions — the case already fails.
			blind(t, SkillValidationCaseRecord{ID: "case-partial", SkillName: "sk", RequiredSubstrings: []string{"kept rule", "never present"}}),
			blind(t, SkillValidationCaseRecord{ID: "case-gain", SkillName: "sk", RequiredSubstrings: []string{"new rule"}}),
		)
		// Candidate keeps the passing assertion and fixes the other case.
		res, err := v.ValidateCandidate("sk", "body with kept rule", "body with kept rule and new rule")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.FlippedCases) != 0 {
			t.Fatalf("no previously-passing case regressed, flips = %v", res.FlippedCases)
		}
		if !res.Pass {
			t.Fatalf("improving candidate without flips must pass: %+v", res)
		}
	})

	t.Run("vacuous case cannot flip", func(t *testing.T) {
		// Blank-only assertions reach the scorer via legacy JSONL data (the
		// tracker rejects them on write today) — they pass vacuously and must
		// never register as a flip.
		cases := []SkillValidationCaseRecord{
			{ID: "case-vacuous", SkillName: "sk", RequiredSubstrings: []string{"   "}, ForbiddenSubstrings: []string{""}},
			{ID: "case-real", SkillName: "sk", RequiredSubstrings: []string{"real rule"}},
		}
		origByCase := scoreSkillValidationCasesByCase("empty body", cases)
		candByCase := scoreSkillValidationCasesByCase("body missing everything", cases)
		for i := range origByCase {
			if origByCase[i].Total > 0 && origByCase[i].casePasses() && !candByCase[i].casePasses() {
				t.Fatalf("case %s flipped — vacuous/failing cases must not flip", cases[i].ID)
			}
		}
		if origByCase[0].Total != 0 || !origByCase[0].casePasses() {
			t.Fatalf("vacuous case should score Total=0 and pass vacuously: %+v", origByCase[0])
		}
	})

	t.Run("identical candidate does not flip", func(t *testing.T) {
		v := engine(
			t,
			blind(t, SkillValidationCaseRecord{ID: "case-a", SkillName: "sk", RequiredSubstrings: []string{"alpha rule"}}),
		)
		res, err := v.ValidateCandidate("sk", "body with alpha rule", "body with alpha rule")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.FlippedCases) != 0 {
			t.Fatalf("identical candidate flipped: %v", res.FlippedCases)
		}
	})
}

// Per-case scores must fold to exactly the aggregate scorer's numbers — the
// flip gate and the score gates must never disagree about what they measured.
func TestScoreSkillValidationCasesByCaseReturnsAggregateScore(t *testing.T) {
	cases := []SkillValidationCaseRecord{
		{ID: "a", SkillName: "sk", RequiredSubstrings: []string{"alpha", ""}, ForbiddenSubstrings: []string{"bad"}},
		{ID: "b", SkillName: "sk", RequiredHeadings: []string{"Setup"}, RequiredSubstrings: []string{"beta"}},
		{ID: "c", SkillName: "sk"},
	}
	for _, body := range []string{"", "alpha", "# Setup\nalpha beta", "bad alpha beta"} {
		agg := scoreSkillValidationCases(body, cases)
		var fold validationCaseScore
		for _, cs := range scoreSkillValidationCasesByCase(body, cases) {
			fold.add(cs)
		}
		if fold.Passed != agg.Passed || fold.Total != agg.Total || fold.Skipped != agg.Skipped {
			t.Fatalf("body %q: fold %+v != aggregate %+v", body, fold, agg)
		}
	}
}
