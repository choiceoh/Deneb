package genesis

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// seedPoolCases records numbered cases until at least one lands in each
// partition pool, returning one known-blind and one known-visible record.
// Membership is derived from the same hash the production split uses, so the
// test stays valid even if the partition ratio changes.
func seedPoolCases(t *testing.T, tr *Tracker, skillName string) (blind, visible SkillValidationCaseRecord) {
	t.Helper()
	var haveBlind, haveVisible bool
	for i := 0; i < 64 && !(haveBlind && haveVisible); i++ {
		rec := SkillValidationCaseRecord{
			SkillName:          skillName,
			ID:                 fmt.Sprintf("pool-case-%d", i),
			RequiredSubstrings: []string{fmt.Sprintf("required marker %d", i)},
		}
		if err := tr.RecordSkillValidationCase(rec); err != nil {
			t.Fatalf("RecordSkillValidationCase: %v", err)
		}
		if validationCaseBlindHeldOut(rec) {
			if !haveBlind {
				blind, haveBlind = rec, true
			}
		} else if !haveVisible {
			visible, haveVisible = rec, true
		}
	}
	if !haveBlind || !haveVisible {
		t.Fatalf("expected both pools to be populated within 64 cases (blind=%v visible=%v)", haveBlind, haveVisible)
	}
	return blind, visible
}

func TestValidationCasePoolPartitionIsDeterministicAndDisjoint(t *testing.T) {
	tr := newTestTracker(t)
	skill := "pool-partition"
	seedPoolCases(t, tr, skill)

	all, err := tr.RecentSkillValidationCases(skill, 100)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	blindPool, err := tr.RecentSkillValidationCasesPool(skill, 100, true)
	if err != nil {
		t.Fatalf("RecentSkillValidationCasesPool(blind): %v", err)
	}
	visiblePool, err := tr.RecentSkillValidationCasesPool(skill, 100, false)
	if err != nil {
		t.Fatalf("RecentSkillValidationCasesPool(visible): %v", err)
	}
	if len(blindPool)+len(visiblePool) != len(all) {
		t.Fatalf("pools must partition the corpus: blind=%d visible=%d all=%d", len(blindPool), len(visiblePool), len(all))
	}
	for _, rec := range blindPool {
		if !validationCaseBlindHeldOut(rec) {
			t.Fatalf("visible-pool case leaked into blind pool: %s", rec.ID)
		}
	}
	for _, rec := range visiblePool {
		if validationCaseBlindHeldOut(rec) {
			t.Fatalf("blind-pool case leaked into visible pool: %s", rec.ID)
		}
	}
}

func TestEvolverPromptNeverShowsBlindPoolCases(t *testing.T) {
	tr := newTestTracker(t)
	skill := "pool-prompt"
	blind, _ := seedPoolCases(t, tr, skill)

	e := &Evolver{tracker: tr, logger: slog.Default()}
	promptCases := e.validationCasesForPrompt(skill)
	for _, rec := range promptCases {
		if rec.ID == blind.ID {
			t.Fatalf("blind held-out case %q must never enter an optimization-side prompt", blind.ID)
		}
		if validationCaseBlindHeldOut(rec) {
			t.Fatalf("prompt helper returned a blind-pool case: %s", rec.ID)
		}
	}
	section := formatValidationCasesForPrompt(promptCases)
	if strings.Contains(section, blind.RequiredSubstrings[0]) {
		t.Fatalf("blind-pool assertion leaked into the producer prompt section")
	}
	// Coverage must still count blind cases the prompt hides.
	if !hasScorableValidationCase(e.validationCasesForCoverage(skill)) {
		t.Fatalf("coverage helper must span both pools")
	}
}

func TestHeldOutGateScoresBlindPoolOnly(t *testing.T) {
	tr := newTestTracker(t)
	skill := "pool-gate"
	seedPoolCases(t, tr, skill)

	engine := NewSkillValidationEngine(tr, slog.Default())
	blindPool, err := tr.RecentSkillValidationCasesPool(skill, 100, true)
	if err != nil {
		t.Fatalf("RecentSkillValidationCasesPool: %v", err)
	}
	gateCases, err := engine.heldOutCases(skill)
	if err != nil {
		t.Fatalf("heldOutCases: %v", err)
	}
	if len(gateCases) != len(blindPool) {
		t.Fatalf("gate must score the blind pool when it is non-empty: got %d cases, blind pool has %d", len(gateCases), len(blindPool))
	}
	for _, rec := range gateCases {
		if !validationCaseBlindHeldOut(rec) {
			t.Fatalf("gate loaded a visible-pool case while blind pool is non-empty: %s", rec.ID)
		}
	}
}

func TestHeldOutGateFallsBackToAllCasesWhenBlindPoolEmpty(t *testing.T) {
	tr := newTestTracker(t)
	skill := "pool-fallback"
	// Record ONLY visible-pool cases by probing IDs until enough land visible.
	recorded := 0
	for i := 0; i < 256 && recorded < 2; i++ {
		rec := SkillValidationCaseRecord{
			SkillName:          skill,
			ID:                 fmt.Sprintf("fallback-case-%d", i),
			RequiredSubstrings: []string{fmt.Sprintf("fallback marker %d", i)},
		}
		if validationCaseBlindHeldOut(rec) {
			continue
		}
		if err := tr.RecordSkillValidationCase(rec); err != nil {
			t.Fatalf("RecordSkillValidationCase: %v", err)
		}
		recorded++
	}
	if recorded == 0 {
		t.Fatalf("could not construct visible-pool-only corpus")
	}
	engine := NewSkillValidationEngine(tr, slog.Default())
	gateCases, err := engine.heldOutCases(skill)
	if err != nil {
		t.Fatalf("heldOutCases: %v", err)
	}
	if len(gateCases) != recorded {
		t.Fatalf("empty blind pool must fall back to all cases: got %d, want %d", len(gateCases), recorded)
	}
}
