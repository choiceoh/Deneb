package genesis

import "testing"

func TestPropusSystemIdentityFiltersWeakSources(t *testing.T) {
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

func TestBuildPropusOverviewUsesUnifiedCoverageAndActions(t *testing.T) {
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

func TestBuildPropusLifecycleSummaryUsesSharedOverview(t *testing.T) {
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
		AgentSkills:       4,
		UnusedAgentSkills: 3,
	})

	if health.State != "attention" {
		t.Fatalf("state = %q, want attention", health.State)
	}
	for _, want := range []string{"recent_rejections", "reviews_all_skipped", "validation_rejections_without_corpus", "many_unused_agent_skills"} {
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
