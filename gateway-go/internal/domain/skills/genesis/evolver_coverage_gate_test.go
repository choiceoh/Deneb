package genesis

import (
	"fmt"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/genbind"
	"strings"
	"testing"
)

// Coverage-conditional relaxation: a skill WITH held-out cases may land bigger
// rewrites (the replay gate measures regressions the size caps only proxied),
// while uncovered skills keep the conservative caps.
func TestCoverageConditionalGates_AllowRelaxedBudgetAndJudgeMarginForCoveredSkills(t *testing.T) {
	var origLines []string
	origLines = append(origLines, "# Skill")
	for i := 0; i < 19; i++ {
		origLines = append(origLines, fmt.Sprintf("- rule %02d", i))
	}
	original := strings.Join(origLines, "\n")

	// Candidate keeps the title + 4 rules and replaces the rest — a changed
	// ratio between the uncovered (0.65) and covered (0.85) caps.
	var candLines []string
	candLines = append(candLines, "# Skill")
	for i := 0; i < 4; i++ {
		candLines = append(candLines, fmt.Sprintf("- rule %02d", i))
	}
	for i := 0; i < 15; i++ {
		candLines = append(candLines, fmt.Sprintf("- refined rule %02d", i))
	}
	candidate := strings.Join(candLines, "\n")

	if ok, _ := genbind.ValidateTextualEditBudget(original, candidate, false); ok {
		t.Fatal("a ~0.75 changed ratio must fail the uncovered budget")
	}
	if ok, reason := genbind.ValidateTextualEditBudget(original, candidate, true); !ok {
		t.Fatalf("a ~0.75 changed ratio should pass the covered budget: %s", reason)
	}

	// Judge margin: +1.5 clears the covered margin (1.0) but not the
	// uncovered one (2.0).
	score := func(v float64) *float64 { return &v }
	verdict := judgeVerdict{Pass: true, OriginalScore: score(70), CandidateScore: score(71.5), Reason: "small win"}
	if pass, _ := acceptJudgeVerdict(verdict, false); pass {
		t.Fatal("+1.5 must not clear the uncovered 2.0 judge margin")
	}
	if pass, reason := acceptJudgeVerdict(verdict, true); !pass {
		t.Fatalf("+1.5 should clear the covered 1.0 judge margin: %s", reason)
	}
}
