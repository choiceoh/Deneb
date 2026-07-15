package skilllifecycle

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
)

func TestSlimSkillLifecycleStatusForAgentOmitsBulkyBodies(t *testing.T) {
	bigBody := strings.Repeat("가", 2500)
	bigFixture := strings.Repeat("x", 3000)
	shortFixture := "Active: active (running)"
	status := chattools.SkillLifecycleStatusResult{
		OK: true,
		Recent: &[]genesis.LifecycleLogEntry{{
			SkillName: "demo",
			Result:    strings.Repeat("r", 900),
			Evidence:  strings.Repeat("e", 900),
		}},
		RejectedEdits: &[]genesis.RejectedSkillEditRecord{{
			SkillName:     "demo",
			Reason:        "gate",
			CandidateBody: bigBody,
		}},
		ValidationCases: &[]genesis.SkillValidationCaseRecord{{
			SkillName: "demo",
			ID:        "case-1",
			Replay: genesis.SkillReplayCaseRecord{
				Input: strings.Repeat("i", 800),
				ExpectedToolCalls: []genesis.SkillReplayToolCallRecord{
					{Name: "wiki", FixtureOutput: bigFixture},
					{Name: "exec", FixtureOutput: shortFixture},
				},
			},
		}},
		SelfCorrectionCandidates: &[]genesis.SelfCorrectionCandidateRecord{{
			ID:             "sc-1",
			Evidence:       strings.Repeat("ev", 500),
			ProposedChange: strings.Repeat("pc", 500),
		}},
	}
	slimSkillLifecycleStatusForAgent(&status)

	if body := (*status.RejectedEdits)[0].CandidateBody; !strings.Contains(body, "omitted from status") {
		t.Fatalf("rejected CandidateBody should be omitted, got %q", body)
	}
	calls := (*status.ValidationCases)[0].Replay.ExpectedToolCalls
	if !strings.Contains(calls[0].FixtureOutput, "omitted from status") {
		t.Fatalf("large FixtureOutput should be omitted, got %q", calls[0].FixtureOutput)
	}
	if calls[1].FixtureOutput != shortFixture {
		t.Fatalf("short FixtureOutput should be kept, got %q", calls[1].FixtureOutput)
	}
	if got := (*status.Recent)[0].Result; len([]rune(got)) > statusSlimResultRunes+3 {
		t.Fatalf("recent Result too long: %d runes", len([]rune(got)))
	}
	if got := (*status.SelfCorrectionCandidates)[0].Evidence; len([]rune(got)) > statusSlimEvidenceRunes+3 {
		t.Fatalf("self-correction Evidence too long: %d runes", len([]rune(got)))
	}
}
