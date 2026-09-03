package propus

import "testing"

func TestBuildPropusSystemIdentityReturnsCanonicalSeparatedFromFilteredSources(t *testing.T) {
	identity := BuildPropusSystemIdentity(PropusScopeGlobal)

	if identity.Name != "Propus" || identity.Codename != "propus" {
		t.Fatalf("identity mismatch: %#v", identity)
	}
	if !containsString(identity.SourcePapers, "arxiv:2606.11459") {
		t.Fatalf("canonical APEX prompt source missing: %#v", identity.SourcePapers)
	}
	if containsString(identity.SourcePapers, "arxiv:2606.15363") {
		t.Fatalf("filtered APEX case-study source promoted to canonical source: %#v", identity.SourcePapers)
	}
	if !containsString(identity.FilteredSources, "arxiv:2606.15363") {
		t.Fatalf("filtered source should stay visible as background evidence: %#v", identity.FilteredSources)
	}
}

func TestBuildPropusOverviewReturnsStateCoverageAndNextActions(t *testing.T) {
	overview := BuildPropusOverview(PropusOverviewInput{
		Scope: PropusScopeGlobal,
		Recent: []LifecycleLogEntry{
			{
				Type:      "evolved",
				SkillName: "deploy-helper",
				SelfHarnessAudit: &HarnessEditAudit{
					TargetSignature:        "deployment misses listener proof",
					EditedSurface:          "SKILL.md verification step",
					ExpectedBehaviorChange: "agent checks the served listener",
					RegressionRisk:         "extra network check latency",
				},
			},
		},
		Stats: []UsageStats{
			{SkillName: "deploy-helper", TotalUses: 3, SuccessCount: 1, FailureCount: 2, SuccessRate: 0.33},
		},
		Curator: []SkillCuratorRecord{
			{SkillName: "deploy-helper", State: SkillCuratorStateActive, CreatedBy: "propus"},
		},
		UsageQuality: UsageQualitySummary{TotalRecords: 3, CountedRecords: 3},
		ValidationSummary: SkillValidationCaseSummary{
			UniqueRecords:            2,
			UniqueEasyAnchorCases:    1,
			UniqueMixedFrontierCases: 1,
			SkillsWithCases:          1,
		},
		Opportunities: []SkillOpportunityRecord{
			{SkillName: "deploy-helper", Route: "evolve"},
		},
		SelfCorrections: []SelfCorrectionCandidateRecord{
			{SkillName: "deploy-helper", Status: SelfCorrectionStatusProposed},
		},
	})

	if overview.State != "needs_review" {
		t.Fatalf("state = %q, want needs_review", overview.State)
	}
	if overview.DoctrineCoverage.State != "covered" {
		t.Fatalf("coverage state = %q, want covered: %#v", overview.DoctrineCoverage.State, overview.DoctrineCoverage)
	}
	if overview.LowSuccessSkills != 1 {
		t.Fatalf("low success skills = %d, want 1", overview.LowSuccessSkills)
	}
	if !containsString(overview.NextActions, "review_pending_self_corrections") {
		t.Fatalf("missing self-correction action: %#v", overview.NextActions)
	}
	if !containsString(overview.NextActions, "triage_low_success_skills") {
		t.Fatalf("missing low-success action: %#v", overview.NextActions)
	}
	if len(overview.DoctrineCoverage.Gaps) != 0 {
		t.Fatalf("unexpected coverage gaps: %#v", overview.DoctrineCoverage.Gaps)
	}
}

func TestBuildPropusLifecycleSummaryReturnsStateCoverageAndNextCue(t *testing.T) {
	summary := BuildPropusLifecycleSummary(PropusLifecycleSummaryInput{
		Scope:     PropusScopeSkill,
		SkillName: "srv1-ops",
		Recent: []LifecycleLogEntry{
			{
				Type:      "evolve_rolled_back",
				SkillName: "srv1-ops",
				CreatedAt: 200,
				SelfHarnessAudit: &HarnessEditAudit{
					TargetSignature:        "srv1 log scan misses real listener state",
					EditedSurface:          "verification checklist",
					ExpectedBehaviorChange: "agent compares log and listener evidence",
					RegressionRisk:         "extra remote read",
				},
			},
		},
		Stats: []UsageStats{
			{SkillName: "srv1-ops", TotalUses: 3, SuccessRate: 0.33},
		},
		ValidationSummary: SkillValidationCaseSummary{SkillName: "srv1-ops"},
	})

	if summary.State != "needs_attention" {
		t.Fatalf("state = %q, want needs_attention", summary.State)
	}
	if summary.DoctrineCoverage.State != "partial" {
		t.Fatalf("coverage state = %q, want partial", summary.DoctrineCoverage.State)
	}
	if summary.NextCue != "기각/롤백 근거 확인" {
		t.Fatalf("next cue = %q", summary.NextCue)
	}
	if summary.LatestType != "evolve_rolled_back" || summary.LatestSkill != "srv1-ops" {
		t.Fatalf("latest mismatch: type=%q skill=%q", summary.LatestType, summary.LatestSkill)
	}
}

func TestBuildPropusHealthPreservesOperationalAttention(t *testing.T) {
	health := BuildPropusHealth(PropusHealthInput{
		Liveness: SkillLivenessState{
			ReviewAttempts:       2,
			ReviewSkips:          2,
			ValidationRejections: 1,
		},
		Evolution:         EvolutionHealthSummary{EvolveRejected7d: 1},
		Validation:        SkillValidationCaseSummary{},
		SelfHarness:       SelfHarnessSignalSummary{SignatureMismatchRejections7d: 1, ValidationDrafts7d: 1, TargetRecurrences7d: 1},
		AgentSkills:       4,
		UnusedAgentSkills: 3,
	})

	if health.State != "attention" {
		t.Fatalf("state = %q, want attention", health.State)
	}
	for _, want := range []string{"recent_rejections", "self_harness_gate_rejections", "self_harness_target_recurrence", "rejected_evolve_validation_drafts", "reviews_all_skipped", "validation_rejections_without_corpus", "many_unused_agent_skills"} {
		if !containsString(health.Attention, want) {
			t.Fatalf("attention missing %q: %#v", want, health.Attention)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// A skill that keeps being delivered, keeps being walked past, and keeps
// succeeding is invisible to every other pressure in the overview: its success
// rate is healthy and it is never idle. The bypass advisory is the only thing
// that can put it in front of an operator.
func TestBuildPropusOverviewRaisesBacklogForBypassedSkillRuns(t *testing.T) {
	overview := BuildPropusOverview(PropusOverviewInput{
		Scope: PropusScopeGlobal,
		Stats: []UsageStats{
			{SkillName: "deploy-helper", TotalUses: 6, SuccessCount: 6, SuccessRate: 1},
		},
		UsageQuality: UsageQualitySummary{
			TotalRecords: 6, CountedRecords: 6,
			BypassedSuccesses: 5, AttributedSuccesses: 6, BypassActionable: true,
		},
		ValidationSummary: SkillValidationCaseSummary{UniqueRecords: 1, SkillsWithCases: 1},
	})

	if overview.BypassedSkillRuns != 5 {
		t.Fatalf("bypassed runs = %d, want 5", overview.BypassedSkillRuns)
	}
	if !containsString(overview.NextActions, "inspect_bypassed_skill_runs") {
		t.Fatalf("missing bypass action: %#v", overview.NextActions)
	}
	// A backlog, not an alarm: nothing failed, so this must not outrank a real
	// failure signal by inflating the state.
	if overview.State != "has_backlog" {
		t.Fatalf("state = %q, want has_backlog", overview.State)
	}
}

// Below the floors the advisory must stay silent — a healthy all-exercised
// skill set has to leave the overview steady.
func TestBuildPropusOverviewOmitsBypassActionBelowFloors(t *testing.T) {
	overview := BuildPropusOverview(PropusOverviewInput{
		Scope: PropusScopeGlobal,
		Stats: []UsageStats{
			{SkillName: "deploy-helper", TotalUses: 6, SuccessCount: 6, SuccessRate: 1},
		},
		UsageQuality: UsageQualitySummary{
			TotalRecords: 6, CountedRecords: 6,
			BypassedSuccesses: 1, AttributedSuccesses: 6, BypassActionable: false,
		},
		ValidationSummary: SkillValidationCaseSummary{UniqueRecords: 1, SkillsWithCases: 1},
	})

	if containsString(overview.NextActions, "inspect_bypassed_skill_runs") {
		t.Fatalf("bypass action raised below the floors: %#v", overview.NextActions)
	}
	if overview.BypassedSkillRuns != 1 {
		t.Fatalf("bypassed runs = %d, want the count reported even when not actionable", overview.BypassedSkillRuns)
	}
}
