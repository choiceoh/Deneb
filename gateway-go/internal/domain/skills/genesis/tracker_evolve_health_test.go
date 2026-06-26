package genesis

import "testing"

// TestEvolutionHealth_SurfacesConfirmAndCrossSkill verifies the two previously
// write-only lifecycle signals (evolve_confirmed, cross_skill_regression) are now
// tallied into EvolutionHealthSummary, with ConfirmRate = confirmed / (confirmed
// + rolled-back).
func TestEvolutionHealth_SurfacesConfirmAndCrossSkill(t *testing.T) {
	tr := newTestTracker(t)
	audit := HarnessEditAudit{
		TargetSignature:        "terminal=timeout|mechanism=bounded-execution",
		EditedSurface:          "Procedure",
		ExpectedBehaviorChange: "pivot after a timeout",
		RegressionRisk:         "low",
	}
	// One evolve that confirms, one that rolls back, one cross-skill regression.
	if err := tr.LogEvolveWithAudit("s1", "1.0.1", "tighten", audit); err != nil {
		t.Fatalf("LogEvolveWithAudit s1: %v", err)
	}
	if err := tr.LogEvolveConfirmed("s1", audit, true); err != nil {
		t.Fatalf("LogEvolveConfirmed: %v", err)
	}
	if err := tr.LogEvolveWithAudit("s2", "1.0.1", "tighten", audit); err != nil {
		t.Fatalf("LogEvolveWithAudit s2: %v", err)
	}
	if err := tr.LogEvolveRolledBack("s2"); err != nil {
		t.Fatalf("LogEvolveRolledBack: %v", err)
	}
	if err := tr.LogCrossSkillRegression("s3", "neighbor", "broke neighbor contract"); err != nil {
		t.Fatalf("LogCrossSkillRegression: %v", err)
	}

	h := tr.EvolutionHealth()
	if h.EvolveConfirmed7d != 1 {
		t.Errorf("EvolveConfirmed7d = %d, want 1", h.EvolveConfirmed7d)
	}
	if h.CrossSkillRegressions7d != 1 {
		t.Errorf("CrossSkillRegressions7d = %d, want 1", h.CrossSkillRegressions7d)
	}
	if h.ConfirmRate != 0.5 { // 1 confirmed / (1 confirmed + 1 rolled back)
		t.Errorf("ConfirmRate = %v, want 0.5", h.ConfirmRate)
	}
}
