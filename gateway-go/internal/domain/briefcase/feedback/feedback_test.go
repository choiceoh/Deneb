package feedback

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFeedbackFirewallBuildsOnlyCoarseDeterministicHandoff(t *testing.T) {
	hidden := HiddenFeedbackInputs{
		SealedSourceIDs:     []string{"sealed-contract-v7"},
		SealedPaths:         []string{"sealed/grader-plan.json"},
		HiddenReferences:    []string{"hidden-ref-31"},
		RubricIDs:           []string{"latest-budget-check"},
		CheckpointIDs:       []string{"checkpoint-private-9"},
		SupervisorReasoning: []string{"The agent ignored the signed amendment."},
		HiddenRationales:    []string{"Critical because the contract controls."},
		ExpectedAnswers:     []string{"The approved budget is 120."},
	}
	firewall, err := NewFeedbackFirewall(hidden, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	input := SimulatorHandoffInput{
		VerdictCategory: VerdictNeedsRevision,
		Recoverable:     true,
		ScoreBand:       ScoreBandMedium,
		VisibleTrajectorySummaries: []string{
			"관련 메일을 확인했고 표 초안을 만들었습니다.",
			"마지막 단계에서 근거 연결이 충분히 드러나지 않았습니다.",
		},
		VisibleArtifactSummaries: []VisibleArtifactSummary{
			{Label: "요약 메모", Status: ArtifactAvailable, Summary: "파일은 열리며 두 개의 표가 보입니다."},
			{Label: "분석표", Status: ArtifactMissing, Summary: "요청된 산출물이 아직 없습니다."},
		},
	}
	handoff, err := firewall.BuildHandoff(input)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.VerdictCategory() != VerdictNeedsRevision || !handoff.Recoverable() || handoff.ScoreBand() != ScoreBandMedium {
		t.Fatalf("coarse fields = %q %v %q", handoff.VerdictCategory(), handoff.Recoverable(), handoff.ScoreBand())
	}
	followUp, err := firewall.SanitizeFollowUp("  지금까지 확인한 공개 자료를 다시 살펴보고, 근거가 보이도록 답을 보완해 주세요.\r\n추가 산출물이 필요하면 작성해 주세요.  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(followUp, "\r") || strings.HasPrefix(followUp, " ") {
		t.Fatalf("follow-up was not canonicalized: %q", followUp)
	}
	artifacts := handoff.VisibleArtifactSummaries()
	if len(artifacts) != 2 || artifacts[0].Label != "분석표" || artifacts[1].Label != "요약 메모" {
		t.Fatalf("artifact order is not canonical: %+v", artifacts)
	}
	// Returned slices must not mutate the handoff.
	trajectory := handoff.VisibleTrajectorySummaries()
	trajectory[0] = "mutated"
	artifacts[0].Summary = "mutated"
	if handoff.VisibleTrajectorySummaries()[0] == "mutated" || handoff.VisibleArtifactSummaries()[0].Summary == "mutated" {
		t.Fatal("handoff getters exposed mutable internal slices")
	}

	first, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	secondHandoff, err := firewall.BuildHandoff(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(secondHandoff)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("handoff JSON is not deterministic:\n%s\n%s", first, second)
	}
	jsonText := string(first)
	for _, forbidden := range []string{
		"sealed-contract-v7", "grader-plan", "hidden-ref-31", "latest-budget-check",
		"checkpoint-private-9", "signed amendment", "contract controls", "approved budget is 120",
		"rubric", "expectedAnswer", "reasoning", "checks", "score\"",
	} {
		if strings.Contains(strings.ToLower(jsonText), strings.ToLower(forbidden)) {
			t.Fatalf("handoff leaked supervisor-only field/token %q: %s", forbidden, jsonText)
		}
	}
	var wire map[string]any
	if err := json.Unmarshal(first, &wire); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"recoverable", "schemaVersion", "scoreBand", "verdictCategory", "visibleArtifactSummaries", "visibleTrajectorySummaries"}
	for _, key := range wantKeys {
		if _, ok := wire[key]; !ok {
			t.Errorf("handoff missing key %q: %v", key, wire)
		}
	}
	if len(wire) != len(wantKeys) {
		t.Fatalf("handoff has unexpected fields: %v", wire)
	}
	if got := fmt.Sprintf("%#v", firewall); strings.Contains(got, "sealed-contract-v7") || strings.Contains(got, "approved budget") {
		t.Fatalf("firewall formatting leaked tokens: %s", got)
	}
}

func TestFeedbackFirewallRejectsEveryHiddenTokenClassWithoutEcho(t *testing.T) {
	tests := []struct {
		name  string
		class ForbiddenFeedbackClass
		make  func(string) HiddenFeedbackInputs
		text  string
	}{
		{"sealed source", ForbiddenSealedSource, func(v string) HiddenFeedbackInputs { return HiddenFeedbackInputs{SealedSourceIDs: []string{v}} }, "Do not mention SECRET-SOURCE-7 again."},
		{"sealed path slash variant", ForbiddenSealedPath, func(v string) HiddenFeedbackInputs { return HiddenFeedbackInputs{SealedPaths: []string{v}} }, `Inspect SEALED\PRIVATE\GOLD.JSON.`},
		{"hidden reference", ForbiddenHiddenReference, func(v string) HiddenFeedbackInputs { return HiddenFeedbackInputs{HiddenReferences: []string{v}} }, "Use hidden-ref-42."},
		{"rubric id", ForbiddenRubricID, func(v string) HiddenFeedbackInputs { return HiddenFeedbackInputs{RubricIDs: []string{v}} }, "You missed RUBRIC-ALPHA."},
		{"checkpoint id", ForbiddenCheckpointID, func(v string) HiddenFeedbackInputs { return HiddenFeedbackInputs{CheckpointIDs: []string{v}} }, "Retry checkpoint-omega."},
		{"supervisor reasoning", ForbiddenSupervisorReasoning, func(v string) HiddenFeedbackInputs { return HiddenFeedbackInputs{SupervisorReasoning: []string{v}} }, "Because the hidden margin is negative, revise."},
		{"hidden rationale", ForbiddenHiddenRationale, func(v string) HiddenFeedbackInputs { return HiddenFeedbackInputs{HiddenRationales: []string{v}} }, "The board memo overrides every other source."},
		{"expected answer", ForbiddenExpectedAnswer, func(v string) HiddenFeedbackInputs { return HiddenFeedbackInputs{ExpectedAnswers: []string{v}} }, "The exact answer is cobalt-17."},
	}
	values := []string{
		"secret-source-7", "sealed/private/gold.json", "hidden-ref-42", "rubric-alpha",
		"checkpoint-omega", "the hidden margin is negative", "the board memo overrides every other source", "cobalt-17",
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			firewall, err := NewFeedbackFirewall(tc.make(values[i]), FeedbackLimits{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = firewall.SanitizeFollowUp(tc.text)
			if !errors.Is(err, ErrFeedbackLeak) {
				t.Fatalf("error = %v, want ErrFeedbackLeak", err)
			}
			var leak *FeedbackLeakError
			if !errors.As(err, &leak) || leak.Class != tc.class {
				t.Fatalf("typed leak = %#v, want class %q", leak, tc.class)
			}
			if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(values[i])) {
				t.Fatalf("leak error echoed hidden token: %v", err)
			}
		})
	}
}

func TestFeedbackFirewallOwnsDenylistAfterConstruction(t *testing.T) {
	hidden := HiddenFeedbackInputs{ExpectedAnswers: []string{"cobalt-17"}}
	firewall, err := NewFeedbackFirewall(hidden, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}

	// The firewall must retain its own normalized denylist, not the caller's
	// mutable slice, so later supervisor bookkeeping cannot change isolation.
	hidden.ExpectedAnswers[0] = "ember-29"
	if _, err := firewall.SanitizeFollowUp("Use cobalt-17."); !errors.Is(err, ErrFeedbackLeak) {
		t.Fatalf("original hidden token was no longer isolated: %v", err)
	}
	if got, err := firewall.SanitizeFollowUp("Use ember-29 instead."); err != nil || got == "" {
		t.Fatalf("caller mutation contaminated denylist: got=%q err=%v", got, err)
	}
}

func TestFeedbackFirewallRejectsUnicodeAndSeparatorAnswerObfuscation(t *testing.T) {
	firewall, err := NewFeedbackFirewall(HiddenFeedbackInputs{ExpectedAnswers: []string{"120", "42"}}, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"zero width":     "Use 1\u200b20.",
		"combining mark": "Use 1\u034f20.",
		"full width":     "Use １２０.",
		"arabic digits":  "Use ١٢٠.",
		"spaces":         "Use 1 2 0.",
		"punctuation":    "Use 1-2-0.",
		"short numeric":  "Use 4 2.",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := firewall.SanitizeFollowUp(text); !errors.Is(err, ErrFeedbackLeak) {
				t.Fatalf("obfuscated answer passed firewall: %q, err=%v", text, err)
			}
		})
	}
}

func TestFeedbackFirewallRejectsShortAndCrossCycleObfuscation(t *testing.T) {
	short, err := NewFeedbackFirewall(HiddenFeedbackInputs{ExpectedAnswers: []string{"OK", "A-B"}}, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Use O-K.", "Use AB."} {
		if _, err := short.SanitizeFollowUp(text); !errors.Is(err, ErrFeedbackLeak) {
			t.Fatalf("short obfuscation passed: %q err=%v", text, err)
		}
	}

	split, err := NewFeedbackFirewall(HiddenFeedbackInputs{ExpectedAnswers: []string{"go-ok"}}, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := split.SanitizeFollowUp("go"); err != nil {
		t.Fatalf("first split fragment should be provisionally accepted: %v", err)
	}
	if _, err := split.SanitizeFollowUp("ok"); !errors.Is(err, ErrFeedbackLeak) {
		t.Fatalf("cross-cycle answer split passed: %v", err)
	}
}

func TestFeedbackFirewallRejectsFoldedPartialAndEquivalentNumericLeaks(t *testing.T) {
	firewall, err := NewFeedbackFirewall(HiddenFeedbackInputs{
		SealedPaths:             []string{"sealed/private/gold.json"},
		ExpectedAnswers:         []string{"The launch code is COBALT-17."},
		ExplicitSensitiveTokens: []string{"COBALT-17", "STRASSE", "1e2"},
	}, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"partial answer":      "Use COBALT-17.",
		"partial sealed path": "Read gold.json.",
		"decomposed accent":   "Use co\u0301balt-17.",
		"full case fold":      "Use Straße.",
		"equivalent number":   "Use 10e1.",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := firewall.SanitizeFollowUp(text); !errors.Is(err, ErrFeedbackLeak) {
				t.Fatalf("equivalent leak passed: %q err=%v", text, err)
			}
		})
	}
}

func TestFeedbackFirewallExtractsSensitiveSealedTextFragments(t *testing.T) {
	firewall, err := NewFeedbackFirewall(HiddenFeedbackInputs{
		SealedContents: []string{"Private evaluator gold: OMEGA-SECRET-731."},
	}, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firewall.SanitizeFollowUp("Use OMEGA-SECRET-731."); !errors.Is(err, ErrFeedbackLeak) {
		t.Fatalf("sealed gold fragment passed firewall: %v", err)
	}
}

func TestFeedbackFirewallScansOnlySimulatorFollowUp(t *testing.T) {
	firewall, err := NewFeedbackFirewall(HiddenFeedbackInputs{}, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firewall.SanitizeFollowUp(`Open sealed/grader-plan.json`); !errors.Is(err, ErrFeedbackLeak) {
		t.Fatalf("follow-up error = %v, want ErrFeedbackLeak", err)
	}
	input := validHandoffInput()
	input.VisibleTrajectorySummaries[0] = "Executor visibly wrote rubric_id=abc and the expected answer phrase."
	input.VisibleArtifactSummaries[0].Label = "checkpoint:public-output"
	input.VisibleArtifactSummaries[0].Summary = "The visible file mentions sealed/grader-plan.json."
	if _, err := firewall.BuildHandoff(input); err != nil {
		t.Fatalf("visible executor output was incorrectly hidden-token scanned: %v", err)
	}
}

func TestFeedbackTextAndLimitsFailClosed(t *testing.T) {
	firewall, err := NewFeedbackFirewall(HiddenFeedbackInputs{}, FeedbackLimits{MaxFollowUpRunes: 4, MaxSummaryRunes: 4, MaxLabelRunes: 4, MaxTrajectoryItems: 1, MaxArtifactItems: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := firewall.SanitizeFollowUp("한글ab"); err != nil || utf8.RuneCountInString(got) != 4 {
		t.Fatalf("exact rune limit = %q, %v", got, err)
	}
	for name, text := range map[string]string{
		"empty":        " \n\t ",
		"invalid utf8": string([]byte{0xff, 'x'}),
		"too long":     "한글abc",
		"control":      "ok\x00",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := firewall.SanitizeFollowUp(text); !errors.Is(err, ErrInvalidFeedback) {
				t.Fatalf("error = %v, want ErrInvalidFeedback", err)
			}
		})
	}

	badLimits := []FeedbackLimits{
		{MaxFollowUpRunes: -1},
		{MaxFollowUpRunes: hardMaxFollowUpRunes + 1},
		{MaxSummaryRunes: hardMaxSummaryRunes + 1},
		{MaxTrajectoryItems: hardMaxTrajectoryItems + 1},
	}
	for _, limits := range badLimits {
		if _, err := NewFeedbackFirewall(HiddenFeedbackInputs{}, limits); !errors.Is(err, ErrInvalidFeedback) {
			t.Errorf("limits %+v error = %v", limits, err)
		}
	}
	for name, hidden := range map[string]HiddenFeedbackInputs{
		"empty token":        {RubricIDs: []string{"  "}},
		"normalized empty":   {HiddenReferences: []string{"\u200b"}},
		"invalid utf8 token": {ExpectedAnswers: []string{string([]byte{0xff})}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewFeedbackFirewall(hidden, FeedbackLimits{}); !errors.Is(err, ErrInvalidFeedback) {
				t.Fatalf("error = %v, want ErrInvalidFeedback", err)
			}
		})
	}
}

func TestFeedbackHandoffShapeValidationAndZeroValueMarshal(t *testing.T) {
	firewall, err := NewFeedbackFirewall(HiddenFeedbackInputs{}, FeedbackLimits{MaxTrajectoryItems: 1, MaxArtifactItems: 1})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*SimulatorHandoffInput)
	}{
		{"verdict", func(in *SimulatorHandoffInput) { in.VerdictCategory = "exact-failure" }},
		{"score band", func(in *SimulatorHandoffInput) { in.ScoreBand = "0.73" }},
		{"no trajectory", func(in *SimulatorHandoffInput) { in.VisibleTrajectorySummaries = nil }},
		{"too many trajectories", func(in *SimulatorHandoffInput) { in.VisibleTrajectorySummaries = []string{"one", "two"} }},
		{"too many artifacts", func(in *SimulatorHandoffInput) {
			in.VisibleArtifactSummaries = append(in.VisibleArtifactSummaries, VisibleArtifactSummary{Label: "b", Status: ArtifactMissing, Summary: "none"})
		}},
		{"artifact status", func(in *SimulatorHandoffInput) { in.VisibleArtifactSummaries[0].Status = "secret-match" }},
		{"duplicate artifact", func(in *SimulatorHandoffInput) {
			in.VisibleArtifactSummaries = append(in.VisibleArtifactSummaries, in.VisibleArtifactSummaries[0])
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := validHandoffInput()
			tc.mutate(&input)
			if _, err := firewall.BuildHandoff(input); !errors.Is(err, ErrInvalidFeedback) {
				t.Fatalf("error = %v, want ErrInvalidFeedback", err)
			}
		})
	}
	if _, err := json.Marshal(SimulatorHandoff{}); !errors.Is(err, ErrInvalidFeedback) {
		t.Fatalf("zero handoff marshal error = %v, want ErrInvalidFeedback", err)
	}
}

func TestFeedbackDefaultLabelLimitCoversManifestIDContract(t *testing.T) {
	firewall, err := NewFeedbackFirewall(HiddenFeedbackInputs{}, FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	input := validHandoffInput()
	input.VisibleArtifactSummaries[0].Label = strings.Repeat("a", 128)
	if _, err := firewall.BuildHandoff(input); err != nil {
		t.Fatalf("valid 128-rune manifest id was rejected: %v", err)
	}
	input.VisibleArtifactSummaries[0].Label += "a"
	if _, err := firewall.BuildHandoff(input); !errors.Is(err, ErrInvalidFeedback) {
		t.Fatalf("129-rune label error = %v", err)
	}
}

func validHandoffInput() SimulatorHandoffInput {
	return SimulatorHandoffInput{
		VerdictCategory:            VerdictNeedsRevision,
		Recoverable:                true,
		ScoreBand:                  ScoreBandMedium,
		VisibleTrajectorySummaries: []string{"Visible work is incomplete."},
		VisibleArtifactSummaries:   []VisibleArtifactSummary{{Label: "report", Status: ArtifactAvailable, Summary: "A draft exists."}},
	}
}
