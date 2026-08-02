package genesis

import (
	"strings"
	"testing"
)

// The disclosure pierces SkillHone blindness, so its gating is the safety
// property: nothing may leak before a PROVEN stall, and nothing that the body
// already satisfies may be named. Separate tests — one loop would hide failures.

func TestTieClassifierMatchesExactTiesOnly(t *testing.T) {
	if !isHeldOutTieRejection("held-out selection rejected: candidate did not improve validation score enough (35.8 vs original 35.8): x") {
		t.Error("an exact tie must classify as tie")
	}
	if isHeldOutTieRejection("held-out selection rejected: candidate did not improve validation score enough (34.1 vs original 35.8): x") {
		t.Error("a real regression is not a tie")
	}
	if isHeldOutTieRejection("behavioral replay regressed: candidate matched 11/45") {
		t.Error("a replay regression is not a tie")
	}
	if isHeldOutTieRejection("judge error") {
		t.Error("an outage is not a tie")
	}
}

func TestFailingRequiredSubstringsNamesOnlyUnmetOnes(t *testing.T) {
	cases := []SkillValidationCaseRecord{
		{RequiredSubstrings: []string{"이미 본문에 있는 문장", "출처별 획득 호출은 합계 최대 2회"}},
		{RequiredSubstrings: []string{"두 번째 미충족 요구", "출처별 획득 호출은 합계 최대 2회"}}, // dup
	}
	body := "# skill\n이미 본문에 있는 문장이 포함된 절차.\n"
	got := failingRequiredSubstrings(cases, body, 3)
	if len(got) != 2 {
		t.Fatalf("want the 2 unmet requirements, got %v", got)
	}
	for _, g := range got {
		if strings.Contains(g, "이미 본문에") {
			t.Errorf("a satisfied requirement must never be disclosed: %v", got)
		}
	}
}

func TestFailingRequiredSubstringsHonorsCap(t *testing.T) {
	cases := []SkillValidationCaseRecord{{RequiredSubstrings: []string{"하나", "둘", "셋", "넷", "다섯"}}}
	if got := failingRequiredSubstrings(cases, "무관한 본문", 3); len(got) != 3 {
		t.Errorf("cap must bound the leak, got %d", len(got))
	}
}

func TestDisclosureStaysSilentBeforeTheStreak(t *testing.T) {
	tr := backoffTracker(t)
	e := &Evolver{tracker: tr}
	// one tie only — not yet a proven stall
	if err := tr.logEvolveRejectedWithProvenance("contract-review",
		"held-out selection rejected: candidate did not improve validation score enough (59.2 vs original 59.2): x",
		HarnessEditAudit{}, nil); err != nil {
		t.Fatal(err)
	}
	if got := e.unmetBlindDisclosure("contract-review", "# body"); got != "" {
		t.Errorf("no disclosure before %d ties: %q", heldOutTieDisclosureStreak, got)
	}
}

func TestDisclosureNamesBlindGapsAfterTheStreak(t *testing.T) {
	tr := backoffTracker(t)
	e := &Evolver{tracker: tr}
	for i := 0; i < heldOutTieDisclosureStreak; i++ {
		if err := tr.logEvolveRejectedWithProvenance("contract-review",
			"held-out selection rejected: candidate did not improve validation score enough (59.2 vs original 59.2): x",
			HarnessEditAudit{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Author cases until one lands in the BLIND pool (membership is a content
	// hash — probe deterministically rather than assuming).
	blindReq := ""
	for i := 0; i < 12 && blindReq == ""; i++ {
		req := "출처별 획득 호출은 합계 최대 2회" + strings.Repeat("!", i)
		rec := SkillValidationCaseRecord{
			SkillName:          "contract-review",
			Description:        "tie disclosure probe",
			RequiredSubstrings: []string{req},
		}
		if err := tr.RecordSkillValidationCase(rec); err != nil {
			t.Fatal(err)
		}
		// membership is derived from the STORED normalized record — read it back
		pool, perr := tr.recentSkillValidationCasesPool("contract-review", 20, true)
		if perr != nil {
			t.Fatal(perr)
		}
		for _, c := range pool {
			for _, r := range c.RequiredSubstrings {
				if r == req {
					blindReq = req
				}
			}
		}
	}
	if blindReq == "" {
		t.Fatal("could not place a case in the blind pool")
	}
	got := e.unmetBlindDisclosure("contract-review", "# 무관한 본문")
	if !strings.Contains(got, blindReq) {
		t.Errorf("after the streak the unmet blind requirement must be named, got %q", got)
	}
	if !strings.Contains(got, "targeted disclosure") {
		t.Error("the section must say it is a deliberate disclosure")
	}
}
