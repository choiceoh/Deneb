package genesis

import (
	"context"
	"log/slog"
	"testing"
)

// Burst keeps evolving while rounds land, stops on the first dry round, and
// never exceeds the cap; a non-evolved first result never bursts.
func TestRunEvolveBurst_LoopUntilDryAndCap(t *testing.T) {
	e := &Evolver{logger: slog.Default()}

	// First round landed; fake rounds: one more accept, then dry.
	seq := []EvolveResult{
		{SkillName: "s", Evolved: true, Reason: "round2"},
		{SkillName: "s", Evolved: false, Reason: "no measurable improvement"},
	}
	calls := 0
	results := e.runEvolveBurst(context.Background(), "s", EvolveResult{SkillName: "s", Evolved: true},
		func(_ context.Context, _ string) (*EvolveResult, error) {
			r := seq[calls]
			calls++
			return &r, nil
		})
	if len(results) != 3 || calls != 2 {
		t.Fatalf("want first+2 rounds ending dry, got %d results / %d calls", len(results), calls)
	}
	if results[2].Evolved {
		t.Fatal("burst must end on the dry round")
	}

	// Always-accepting rounds hit the cap: first + (cap-1) rounds.
	calls = 0
	results = e.runEvolveBurst(context.Background(), "s", EvolveResult{Evolved: true},
		func(_ context.Context, _ string) (*EvolveResult, error) {
			calls++
			return &EvolveResult{SkillName: "s", Evolved: true}, nil
		})
	if len(results) != skillEvolveBurstMaxRounds || calls != skillEvolveBurstMaxRounds-1 {
		t.Fatalf("cap should bound the burst: %d results / %d calls", len(results), calls)
	}

	// A non-evolved first result never bursts.
	calls = 0
	results = e.runEvolveBurst(context.Background(), "s", EvolveResult{Evolved: false},
		func(_ context.Context, _ string) (*EvolveResult, error) {
			calls++
			return &EvolveResult{Evolved: true}, nil
		})
	if len(results) != 1 || calls != 0 {
		t.Fatalf("rejected first round must not burst: %d results / %d calls", len(results), calls)
	}
}

// A2/A4: "covered" (relaxed caps + burst continuation) requires a SCORABLE
// case, not mere case existence — an assertion-less corpus must read uncovered.
func TestHasScorableValidationCase(t *testing.T) {
	if hasScorableValidationCase(nil) {
		t.Fatal("no cases must read uncovered")
	}
	assertionless := []SkillValidationCaseRecord{{SkillName: "s", ID: "x"}}
	if hasScorableValidationCase(assertionless) {
		t.Fatal("an assertion-less case grants no real regression gate → uncovered")
	}
	scorable := []SkillValidationCaseRecord{{
		SkillName: "s", ID: "y",
		Replay: SkillReplayCaseRecord{Input: "do x", RequiredTools: []string{"fs"}},
	}}
	if !hasScorableValidationCase(scorable) {
		t.Fatal("a case with assertions is covered")
	}
}
