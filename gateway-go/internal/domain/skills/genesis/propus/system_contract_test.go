package propus

import (
	"reflect"
	"strings"
	"testing"
)

func TestNormalizePropusScopeMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{
			name:  "empty",
			input: "",
			want:  "global",
		},
		{
			name:  "global",
			input: "global",
			want:  "global",
		},
		{
			name:  "skill",
			input: "skill",
			want:  "skill",
		},
		{
			name:  "trim-skill",
			input: " skill ",
			want:  "skill",
		},
		{
			name:  "upper",
			input: "SKILL",
			want:  "global",
		},
		{
			name:  "mixed",
			input: "Skill",
			want:  "global",
		},
		{
			name:  "newline",
			input: "\nskill\n",
			want:  "skill",
		},
		{
			name:  "tab",
			input: "\tskill\t",
			want:  "skill",
		},
		{
			name:  "unknown",
			input: "project",
			want:  "global",
		},
		{
			name:  "space",
			input: " ",
			want:  "global",
		},
		{
			name:  "plural",
			input: "skills",
			want:  "global",
		},
		{
			name:  "prefix",
			input: "skill:x",
			want:  "global",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizePropusScope(tc.input); got != tc.want {
				t.Fatalf("normalizePropusScope(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestPropusStatePriorityReturnsRanksForKnownStatesCaseSensitively(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, state string
		want        int
	}{
		{
			name:  "attention",
			state: "needs_attention",
			want:  5,
		},
		{
			name:  "review",
			state: "needs_review",
			want:  4,
		},
		{
			name:  "evolution",
			state: "needs_evolution",
			want:  3,
		},
		{
			name:  "validation",
			state: "needs_validation",
			want:  2,
		},
		{
			name:  "backlog",
			state: "has_backlog",
			want:  1,
		},
		{
			name:  "steady",
			state: "steady",
			want:  0,
		},
		{
			name:  "idle",
			state: "idle",
			want:  0,
		},
		{
			name:  "empty",
			state: "",
			want:  0,
		},
		{
			name:  "unknown",
			state: "unknown",
			want:  0,
		},
		{
			name:  "case",
			state: "NEEDS_ATTENTION",
			want:  0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := propusStatePriority(tc.state); got != tc.want {
				t.Fatalf("propusStatePriority(%q) = %d, want %d", tc.state, got, tc.want)
			}
		})
	}
}

func TestPropusMaxStateReturnsHigherPriorityStateForEveryPair(t *testing.T) {
	t.Parallel()
	states := []string{"steady", "has_backlog", "needs_validation", "needs_evolution", "needs_review", "needs_attention"}
	for _, current := range states {
		for _, candidate := range states {
			t.Run(current+"__"+candidate, func(t *testing.T) {
				t.Parallel()
				want := current
				if propusStatePriority(candidate) > propusStatePriority(current) {
					want = candidate
				}
				if got := propusMaxState(current, candidate); got != want {
					t.Fatalf("propusMaxState(%q, %q) = %q, want %q", current, candidate, got, want)
				}
			})
		}
	}
}

func TestPropusActionPriorityReturnsRanksForKnownActionsCaseSensitively(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, action string
		want         int
	}{
		{
			name:   "reject-skill",
			action: "inspect_rejected_or_rolled_back_edits",
			want:   60,
		},
		{
			name:   "reject-global",
			action: "inspect_recent_rejections_and_rollbacks",
			want:   60,
		},
		{
			name:   "recurrence",
			action: "inspect_self_harness_target_recurrences",
			want:   60,
		},
		{
			name:   "self-correction",
			action: "review_pending_self_corrections",
			want:   50,
		},
		{
			name:   "draft",
			action: "review_rejected_evolve_validation_drafts",
			want:   50,
		},
		{
			name:   "evolve-skill",
			action: "inspect_usage_failures_and_propose_evolve",
			want:   40,
		},
		{
			name:   "evolve-global",
			action: "triage_low_success_skills",
			want:   40,
		},
		{
			name:   "record-case",
			action: "record_validation_case_from_session",
			want:   30,
		},
		{
			name:   "backfill",
			action: "backfill_validation_cases_for_active_skills",
			want:   30,
		},
		{
			name:   "audit",
			action: "require_failure_signature_audit_before_next_evolve",
			want:   30,
		},
		{
			name:   "tier",
			action: "tag_validation_cases_easy_mixed_hard",
			want:   30,
		},
		{
			name:   "collect",
			action: "collect_more_validation_cases_before_large_rewrite",
			want:   30,
		},
		{
			name:   "backlog-skill",
			action: "triage_opportunity_backlog",
			want:   20,
		},
		{
			name:   "backlog-global",
			action: "route_opportunity_backlog",
			want:   20,
		},
		{
			name:   "near-miss",
			action: "record_repeated_noop_or_near_miss_as_opportunity",
			want:   20,
		},
		{
			name:   "bypass",
			action: "inspect_bypassed_skill_runs",
			want:   20,
		},
		{
			name:   "continue",
			action: "continue_observing",
			want:   0,
		},
		{
			name:   "empty",
			action: "",
			want:   0,
		},
		{
			name:   "unknown",
			action: "something_else",
			want:   0,
		},
		{
			name:   "case",
			action: "TRIAGE_LOW_SUCCESS_SKILLS",
			want:   0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := propusActionPriority(tc.action); got != tc.want {
				t.Fatalf("propusActionPriority(%q) = %d, want %d", tc.action, got, tc.want)
			}
		})
	}
}

func TestPropusPrioritizeNextActionsOrdersFirstActionByStateWithoutMutatingInput(t *testing.T) {
	t.Parallel()
	all := []string{"continue_observing", "route_opportunity_backlog", "record_validation_case_from_session", "triage_low_success_skills", "review_pending_self_corrections", "inspect_recent_rejections_and_rollbacks"}
	cases := []struct{ state, first string }{
		{"needs_attention", "inspect_recent_rejections_and_rollbacks"},
		{"needs_review", "review_pending_self_corrections"},
		{"needs_evolution", "triage_low_success_skills"},
		{"needs_validation", "record_validation_case_from_session"},
		{"has_backlog", "route_opportunity_backlog"},
		{"steady", "continue_observing"},
	}
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			t.Parallel()
			input := append([]string(nil), all...)
			got := propusPrioritizeNextActions(tc.state, input)
			if len(got) != len(all) || got[0] != tc.first {
				t.Fatalf("prioritized = %#v, want first %q", got, tc.first)
			}
			if !reflect.DeepEqual(input, all) {
				t.Fatalf("input mutated: %#v", input)
			}
		})
	}
}

func TestAppendUniqueStringsTrimsDedupesCaseInsensitivelyAndPreservesOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name               string
		base, values, want []string
	}{
		{
			name:   "empty",
			base:   nil,
			values: nil,
			want:   nil,
		},
		{
			name:   "trim",
			base:   []string{" alpha "},
			values: []string{" beta "},
			want:   []string{"alpha", "beta"},
		},
		{
			name:   "case-dedupe",
			base:   []string{"Alpha"},
			values: []string{"alpha", "ALPHA"},
			want:   []string{"Alpha"},
		},
		{
			name:   "drop-empty",
			base:   []string{"", "a"},
			values: []string{" ", "b"},
			want:   []string{"a", "b"},
		},
		{
			name:   "stable-order",
			base:   []string{"b", "a"},
			values: []string{"c", "a", "d"},
			want:   []string{"b", "a", "c", "d"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := appendUniqueStrings(tc.base, tc.values...)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("appendUniqueStrings() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestPropusNextCueSelectsMessageByFirstMatchingActionOrStateFallback(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, state string
		actions     []string
		want        string
	}{
		{
			name:    "correction",
			state:   "steady",
			actions: []string{"review_pending_self_corrections"},
			want:    "자가수정 후보를 먼저 리뷰하세요",
		},
		{
			name:    "draft",
			state:   "steady",
			actions: []string{"review_rejected_evolve_validation_drafts"},
			want:    "반려된 evolve 후보를 held-out validation으로 승격할지 리뷰하세요",
		},
		{
			name:    "case",
			state:   "steady",
			actions: []string{"record_validation_case_from_session"},
			want:    "검증 케이스를 기록해 생성/진화 debt를 줄이세요",
		},
		{
			name:    "backfill",
			state:   "steady",
			actions: []string{"backfill_validation_cases_for_active_skills"},
			want:    "검증 케이스를 기록해 생성/진화 debt를 줄이세요",
		},
		{
			name:    "evolve",
			state:   "steady",
			actions: []string{"inspect_usage_failures_and_propose_evolve"},
			want:    "낮은 성공률 근거를 확인하고 evolve 후보를 좁히세요",
		},
		{
			name:    "triage",
			state:   "steady",
			actions: []string{"triage_low_success_skills"},
			want:    "낮은 성공률 근거를 확인하고 evolve 후보를 좁히세요",
		},
		{
			name:    "reject-skill",
			state:   "steady",
			actions: []string{"inspect_rejected_or_rolled_back_edits"},
			want:    "기각/롤백 근거 확인",
		},
		{
			name:    "reject-global",
			state:   "steady",
			actions: []string{"inspect_recent_rejections_and_rollbacks"},
			want:    "기각/롤백 근거 확인",
		},
		{
			name:    "recurrence",
			state:   "steady",
			actions: []string{"inspect_self_harness_target_recurrences"},
			want:    "Self-Harness target signature 재발을 확인하세요",
		},
		{
			name:    "backlog-skill",
			state:   "steady",
			actions: []string{"triage_opportunity_backlog"},
			want:    "opportunity backlog를 route/evolve/no-op로 정리하세요",
		},
		{
			name:    "backlog-global",
			state:   "steady",
			actions: []string{"route_opportunity_backlog"},
			want:    "opportunity backlog를 route/evolve/no-op로 정리하세요",
		},
		{
			name:    "tier",
			state:   "steady",
			actions: []string{"tag_validation_cases_easy_mixed_hard"},
			want:    "validation case에 easy/mixed/hard frontier tier를 붙이세요",
		},
		{
			name:    "bypass",
			state:   "steady",
			actions: []string{"inspect_bypassed_skill_runs"},
			want:    "성공했지만 스킬 절차가 실행되지 않은 런을 확인하세요 — 트리거가 넓거나 본문이 낡았습니다",
		},
		{
			name:    "idle",
			state:   "idle",
			actions: []string{},
			want:    "Propus 활동이 쌓이면 생성/진화/리뷰 압력을 요약합니다",
		},
		{
			name:    "steady",
			state:   "steady",
			actions: []string{},
			want:    "최근 생성/진화가 검증 근거와 연결되는지 확인",
		},
		{
			name:    "fallback",
			state:   "needs_review",
			actions: []string{},
			want:    "Propus overview.nextActions를 우선 처리하세요",
		},
		{
			name:    "unknown-action-idle",
			state:   "idle",
			actions: []string{"unknown"},
			want:    "Propus 활동이 쌓이면 생성/진화/리뷰 압력을 요약합니다",
		},
		{
			name:    "unknown-action-steady",
			state:   "steady",
			actions: []string{"unknown"},
			want:    "최근 생성/진화가 검증 근거와 연결되는지 확인",
		},
		{
			name:    "first-wins",
			state:   "idle",
			actions: []string{"unknown", "review_pending_self_corrections"},
			want:    "Propus 활동이 쌓이면 생성/진화/리뷰 압력을 요약합니다",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := PropusNextCue(tc.state, tc.actions); got != tc.want {
				t.Fatalf("PropusNextCue(%q, %#v) = %q, want %q", tc.state, tc.actions, got, tc.want)
			}
		})
	}
}

func TestPropusLifecycleEventTypeNormalizesKnownTypesElseReview(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, input, want string }{
		{
			name:  "empty",
			input: "",
			want:  "review",
		},
		{
			name:  "genesis",
			input: "genesis",
			want:  "genesis",
		},
		{
			name:  "evolved",
			input: "evolved",
			want:  "evolved",
		},
		{
			name:  "rejected",
			input: "evolve_rejected",
			want:  "evolve_rejected",
		},
		{
			name:  "rollback",
			input: "evolve_rolled_back",
			want:  "evolve_rolled_back",
		},
		{
			name:  "proposal",
			input: "evolution_proposal",
			want:  "review",
		},
		{
			name:  "unknown",
			input: "other",
			want:  "review",
		},
		{
			name:  "spaces",
			input: " genesis ",
			want:  "review",
		},
		{
			name:  "case",
			input: "GENESIS",
			want:  "review",
		},
		{
			name:  "blank-space",
			input: "   ",
			want:  "review",
		},
		{
			name:  "review",
			input: "review",
			want:  "review",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := PropusLifecycleEventType(LifecycleLogEntry{Type: tc.input}); got != tc.want {
				t.Fatalf("event type = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPropusLastActivityMSReturnsMaxOfFourTimestampFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		live SkillLivenessState
		want int64
	}{
		{name: "zero", live: SkillLivenessState{}, want: 0},
		{name: "review", live: SkillLivenessState{LastReviewAt: 9, LastEvolveAt: 8, LastGenesisAt: 7, LastErrorAt: 6}, want: 9},
		{name: "evolve", live: SkillLivenessState{LastReviewAt: 8, LastEvolveAt: 9, LastGenesisAt: 7, LastErrorAt: 6}, want: 9},
		{name: "genesis", live: SkillLivenessState{LastReviewAt: 8, LastEvolveAt: 7, LastGenesisAt: 9, LastErrorAt: 6}, want: 9},
		{name: "error", live: SkillLivenessState{LastReviewAt: 8, LastEvolveAt: 7, LastGenesisAt: 6, LastErrorAt: 9}, want: 9},
		{name: "ties", live: SkillLivenessState{LastReviewAt: 9, LastEvolveAt: 9, LastGenesisAt: 9, LastErrorAt: 9}, want: 9},
		{name: "negative", live: SkillLivenessState{LastReviewAt: -1, LastEvolveAt: -2, LastGenesisAt: -3, LastErrorAt: -4}, want: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := PropusLastActivityMS(tc.live); got != tc.want {
				t.Fatalf("last activity = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPropusLifecycleCountsReturnsEntriesByTypeAndExecution(t *testing.T) {
	t.Parallel()
	entries := []LifecycleLogEntry{
		{Type: ""},
		{Type: "genesis"},
		{Type: "evolved", SelfHarnessAudit: &HarnessEditAudit{TargetSignature: "sig"}},
		{Type: "evolve_rejected"},
		{Type: "evolve_rolled_back", SelfHarnessAudit: &HarnessEditAudit{}},
		{Type: "evolution_proposal", Executed: true},
		{Type: "evolution_proposal", Executed: false},
		{Type: "other"},
	}
	got := PropusLifecycleCounts(entries)
	want := map[string]int{
		"genesis":              2,
		"review":               2,
		"evolved":              1,
		"evolve_rejected":      1,
		"evolve_rolled_back":   1,
		"other":                1,
		"executed_review":      1,
		"non_executed_review":  1,
		"validation_sensitive": 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("counts = %#v, want %#v", got, want)
	}
}

func TestPropusHealthAttentionReturnsEachSignalIndependently(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input PropusHealthInput
		want  string
	}{
		{name: "last-error", input: PropusHealthInput{Liveness: SkillLivenessState{LastError: "boom"}}, want: "last_error"},
		{name: "thrash", input: PropusHealthInput{Evolution: EvolutionHealthSummary{Thrash: true}}, want: "evolve_thrash"},
		{name: "rollback", input: PropusHealthInput{Evolution: EvolutionHealthSummary{EvolveRolledBack7d: 1}}, want: "recent_rollbacks"},
		{name: "rejection", input: PropusHealthInput{Evolution: EvolutionHealthSummary{EvolveRejected7d: 1}}, want: "recent_rejections"},
		{name: "recurrence", input: PropusHealthInput{SelfHarness: SelfHarnessSignalSummary{TargetRecurrences7d: 1}}, want: "self_harness_target_recurrence"},
		{name: "signature", input: PropusHealthInput{SelfHarness: SelfHarnessSignalSummary{SignatureMismatchRejections7d: 1}}, want: "self_harness_gate_rejections"},
		{name: "surface", input: PropusHealthInput{SelfHarness: SelfHarnessSignalSummary{SurfaceMismatchRejections7d: 1}}, want: "self_harness_gate_rejections"},
		{name: "draft", input: PropusHealthInput{SelfHarness: SelfHarnessSignalSummary{ValidationDrafts7d: 1}}, want: "rejected_evolve_validation_drafts"},
		{name: "all-skipped", input: PropusHealthInput{Liveness: SkillLivenessState{ReviewAttempts: 2, ReviewSkips: 2}}, want: "reviews_all_skipped"},
		{name: "rejected-no-corpus", input: PropusHealthInput{Liveness: SkillLivenessState{ValidationRejections: 1}}, want: "validation_rejections_without_corpus"},
		{name: "unused-half", input: PropusHealthInput{AgentSkills: 4, UnusedAgentSkills: 2}, want: "many_unused_agent_skills"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := propusHealthAttention(tc.input)
			if !containsStringForContract(got, tc.want) {
				t.Fatalf("attention = %#v, want %q", got, tc.want)
			}
		})
	}
}

func containsStringForContract(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestBuildPropusHealthStateReturnsEscalatingIdleObservingAttentionDegraded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input PropusHealthInput
		want  string
	}{
		{name: "idle", input: PropusHealthInput{}, want: "idle"},
		{name: "observing-time", input: PropusHealthInput{Liveness: SkillLivenessState{LastReviewAt: 1}}, want: "observing"},
		{name: "observing-attempt", input: PropusHealthInput{Liveness: SkillLivenessState{ReviewAttempts: 1, LastReviewAt: 1}}, want: "observing"},
		{name: "attention", input: PropusHealthInput{Evolution: EvolutionHealthSummary{Thrash: true}}, want: "attention"},
		{name: "degraded-over-attention", input: PropusHealthInput{Liveness: SkillLivenessState{LastError: "boom"}, Evolution: EvolutionHealthSummary{Thrash: true}}, want: "degraded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := BuildPropusHealth(tc.input); got.State != tc.want {
				t.Fatalf("state = %q, want %q (%#v)", got.State, tc.want, got)
			}
		})
	}
}
