package genesis

import (
	"log/slog"
	"testing"
)

// ① Behavioral validation upgrade: a reproduction case gains a tool-call
// (behavioral) assertion from the targeted failure trace, so the strongest
// gate tests "does the skill make the agent DO X", not just "does the body
// say X".
func TestEnrichReproductionWithBehavior(t *testing.T) {
	base := func() *SkillValidationCaseRecord {
		return &SkillValidationCaseRecord{SkillName: "sk", RequiredSubstrings: []string{"corrected step"}}
	}

	t.Run("tool-error trace becomes a forbidden tool call + action", func(t *testing.T) {
		rc := base()
		stats := &UsageStats{RecentFailureTraces: []UsageFailureTrace{
			{ToolName: "wiki_search", ToolInput: "q=very long query that timed out", ToolError: true, Signature: "wiki_search timeout on long queries"},
		}}
		enrichReproductionWithBehavior(rc, stats)
		if len(rc.Replay.ForbiddenToolCalls) != 1 || rc.Replay.ForbiddenToolCalls[0].Name != "wiki_search" {
			t.Fatalf("forbidden tool call not attached: %+v", rc.Replay.ForbiddenToolCalls)
		}
		if !rc.Replay.ForbiddenToolCalls[0].FixtureError {
			t.Fatal("failed tool call must be marked FixtureError")
		}
		if len(rc.Replay.ForbiddenActions) != 1 {
			t.Fatalf("forbidden action (signature) not attached: %+v", rc.Replay.ForbiddenActions)
		}
		if rc.Replay.Input == "" {
			t.Fatal("replay input (task to simulate) not set — executor gate cannot run without it")
		}
		// The case must now be behaviorally evaluable (executor gate can pick it up).
		if !replayBehaviorEvaluable(rc.Replay) {
			t.Fatal("enriched case is not behaviorally evaluable")
		}
	})

	t.Run("non-error trace (no tool defect) leaves the case string-only", func(t *testing.T) {
		rc := base()
		stats := &UsageStats{RecentFailureTraces: []UsageFailureTrace{
			{ToolName: "wiki_search", ToolError: false}, // succeeded — not the defect
			{ErrorMsg: "generic failure, no tool named"},
		}}
		enrichReproductionWithBehavior(rc, stats)
		if len(rc.Replay.ForbiddenToolCalls) != 0 || len(rc.Replay.ForbiddenActions) != 0 {
			t.Fatalf("string-only case got spurious behavioral assertions: %+v", rc.Replay)
		}
	})

	t.Run("nil inputs are safe", func(t *testing.T) {
		enrichReproductionWithBehavior(nil, &UsageStats{})
		enrichReproductionWithBehavior(base(), nil)
	})

	t.Run("only the first tool-error trace enriches (one behavioral signal)", func(t *testing.T) {
		rc := base()
		stats := &UsageStats{RecentFailureTraces: []UsageFailureTrace{
			{ToolName: "tool_a", ToolError: true, Signature: "a failed"},
			{ToolName: "tool_b", ToolError: true, Signature: "b failed"},
		}}
		enrichReproductionWithBehavior(rc, stats)
		if len(rc.Replay.ForbiddenToolCalls) != 1 || rc.Replay.ForbiddenToolCalls[0].Name != "tool_a" {
			t.Fatalf("expected exactly the first tool-error trace: %+v", rc.Replay.ForbiddenToolCalls)
		}
	})
}

// The oracle's fails-on-original/passes-on-candidate check must ignore the
// behavioral assertions: a candidate that legitimately still names the failing
// tool must NOT cause the (string-discriminative) case to be dropped.
func TestAdoptReproductionCaseKeepsBehavioralAssertionsWithoutAffectingOracleDiscrimination(t *testing.T) {
	newEvolver := func(t *testing.T) *Evolver {
		t.Helper()
		t.Setenv("HOME", t.TempDir())
		t.Setenv("DENEB_STATE_DIR", t.TempDir())
		tr, err := NewTracker(nil)
		if err != nil {
			t.Fatal(err)
		}
		return &Evolver{tracker: tr, logger: slog.Default()}
	}
	e := newEvolver(t)
	rc := &SkillValidationCaseRecord{
		SkillName:          "sk",
		ID:                 "repro-sk-1.0.1",
		RequiredSubstrings: []string{"timeout 파라미터"},
		Source:             "reproduction-oracle",
		FrontierTier:       "hard",
	}
	// Behavioral enrichment forbids the wiki_search tool call — but BOTH bodies
	// legitimately still name wiki_search (the fix adds a timeout, not removes
	// the tool). String assertion is discriminative; behavioral is not.
	enrichReproductionWithBehavior(rc, &UsageStats{RecentFailureTraces: []UsageFailureTrace{
		{ToolName: "wiki_search", ToolError: true, Signature: "wiki_search timeout"},
	}})
	original := "wiki_search 를 그대로 호출한다"
	candidate := "wiki_search 를 timeout 파라미터와 함께 호출한다"
	e.adoptReproductionCase("sk", original, candidate, rc)

	got, err := e.tracker.RecentSkillValidationCases("sk", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("case dropped despite discriminative string assertion: %+v", got)
	}
	// The recorded case DID keep its behavioral assertions for the executor gate.
	if len(got[0].Replay.ForbiddenToolCalls) != 1 {
		t.Fatalf("behavioral assertions lost from the recorded case: %+v", got[0].Replay)
	}
}
