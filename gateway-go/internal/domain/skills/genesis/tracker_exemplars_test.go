package genesis

import (
	"log/slog"
	"strings"
	"testing"
)

func TestConfirmedEvolveExemplars_RetrievalContract(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	confirm := func(skill, sig string) {
		t.Helper()
		if err := tr.logEvolveConfirmed(skill, HarnessEditAudit{TargetSignature: sig, EditedSurface: "Procedure", ExpectedBehaviorChange: "회상 선행"}, true); err != nil {
			t.Fatal(err)
		}
	}
	confirm("skill-a", "wiki search returns empty")
	confirm("skill-b", "Wiki  Search Returns EMPTY") // 대소문자/공백 정규화 일치
	confirm("skill-c", "완전히 다른 실패")
	confirm("skill-self", "wiki search returns empty") // 자기 자신 — 제외 대상

	got, err := tr.confirmedEvolveExemplars([]string{"wiki search returns empty"}, "skill-self", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("exemplars = %d, want 2 (normalized cross-skill matches only): %+v", len(got), got)
	}
	for _, ex := range got {
		if ex.SkillName == "skill-self" || ex.SkillName == "skill-c" {
			t.Fatalf("retrieval leaked %q", ex.SkillName)
		}
	}
	// newest first
	if got[0].SkillName != "skill-b" {
		t.Fatalf("order not newest-first: %+v", got)
	}
	// limit + empty inputs
	if one, _ := tr.confirmedEvolveExemplars([]string{"wiki search returns empty"}, "", 1); len(one) != 1 {
		t.Fatalf("limit not applied: %+v", one)
	}
	if none, _ := tr.confirmedEvolveExemplars(nil, "", 3); none != nil {
		t.Fatalf("no signatures must return nil: %+v", none)
	}
}

func TestFormatConfirmedEvolveExemplars(t *testing.T) {
	if formatConfirmedEvolveExemplars(nil) != "" {
		t.Fatal("empty exemplars must render nothing")
	}
	out := formatConfirmedEvolveExemplars([]confirmedEvolveExemplar{{
		SkillName: "skill-a",
		Audit:     HarnessEditAudit{TargetSignature: "sig", EditedSurface: "Procedure", ExpectedBehaviorChange: "회상 선행"},
	}})
	for _, want := range []string{"검증 완주한 개선 사례", "[skill-a]", "Procedure", "회상 선행"} {
		if !strings.Contains(out, want) {
			t.Fatalf("section missing %q:\n%s", want, out)
		}
	}
}

func TestEvolutionHealth_FalseAcceptScoreboard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("a", HarnessEditAudit{}, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("b", HarnessEditAudit{}, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveRolledBack("c"); err != nil {
		t.Fatal(err)
	}
	h := tr.EvolutionHealth()
	if h.ResolvedEvolves7d != 3 {
		t.Fatalf("resolved n = %d, want 3", h.ResolvedEvolves7d)
	}
	if h.FalseAcceptRate < 0.33 || h.FalseAcceptRate > 0.34 {
		t.Fatalf("falseAcceptRate = %v, want 1/3", h.FalseAcceptRate)
	}
	if diff := h.ConfirmRate + h.FalseAcceptRate; diff < 0.999 || diff > 1.001 {
		t.Fatalf("rates must complement: %v + %v", h.ConfirmRate, h.FalseAcceptRate)
	}
}

// Mechanism-level fallback (ToE 2606.06960): when no confirmed evolve matches
// the exact signature, one sharing the mechanism=… component still surfaces;
// unrelated mechanisms stay excluded.
func TestConfirmedEvolveExemplars_MechanismFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("sk-donor", HarnessEditAudit{
		TargetSignature: "terminal=timeout|mechanism=tool-plan-drift",
	}, true); err != nil {
		t.Fatal(err)
	}
	// Different terminal, same mechanism → only the fallback can find it.
	got, err := tr.confirmedEvolveExemplars([]string{"terminal=crash|mechanism=tool-plan-drift"}, "sk-target", 3)
	if err != nil || len(got) != 1 || got[0].SkillName != "sk-donor" {
		t.Fatalf("mechanism fallback missed the donor: %+v err=%v", got, err)
	}
	// No shared mechanism → nothing.
	if got, _ := tr.confirmedEvolveExemplars([]string{"terminal=crash|mechanism=other"}, "sk-target", 3); len(got) != 0 {
		t.Fatalf("unrelated mechanism must not match: %+v", got)
	}
}
