package genesis

import (
	"sort"
	"strings"
)

const (
	PropusScopeGlobal = "global"
	PropusScopeSkill  = "skill"
)

// PropusSystemIdentity is the shared identity and source contract for every
// Propus surface. Runtime handlers should project this value, not rebuild their
// own doctrine view.
type PropusSystemIdentity struct {
	Name               string                `json:"name"`
	Codename           string                `json:"codename"`
	Version            string                `json:"version"`
	Tool               string                `json:"tool"`
	Scope              string                `json:"scope"`
	Description        string                `json:"description"`
	Loop               []string              `json:"loop"`
	SourcePapers       []string              `json:"sourcePapers"`
	FilteredSources    []string              `json:"filteredSources"`
	Principles         []string              `json:"principles"`
	Invariants         []string              `json:"invariants"`
	QualityGates       []string              `json:"qualityGates"`
	SourcePrinciples   []PropusDoctrinePaper `json:"sourcePrinciples"`
	FilteredPrinciples []PropusDoctrinePaper `json:"filteredPrinciples"`
}

func BuildPropusSystemIdentity(scope string) PropusSystemIdentity {
	doctrine := PropusDoctrine()
	scope = normalizePropusScope(scope)
	return PropusSystemIdentity{
		Name:               doctrine.Name,
		Codename:           doctrine.Codename,
		Version:            doctrine.Version,
		Tool:               "skill_lifecycle",
		Scope:              scope,
		Description:        "Deneb closed-loop self-improvement: observe work, propose reusable changes, validate with held-out replay cases, evolve or generate skills, watch outcomes, rollback regressions, and queue deferred self-corrections.",
		Loop:               doctrine.Lifecycle,
		SourcePapers:       doctrine.SourceIDs(),
		FilteredSources:    doctrine.FilteredSourceIDs(),
		Principles:         doctrine.ProductRules(),
		Invariants:         doctrine.Invariants,
		QualityGates:       doctrine.QualityGates,
		SourcePrinciples:   doctrine.Papers,
		FilteredPrinciples: doctrine.FilteredPapers,
	}
}

// PropusDoctrineCoverage is the single evidence-readiness model behind the
// status tool, native summary, and health payload.
type PropusDoctrineCoverage struct {
	State              string         `json:"state"`
	Covered            []string       `json:"covered"`
	Gaps               []string       `json:"gaps"`
	AxisCoverage       map[string]int `json:"axisCoverage"`
	SourcePolicy       string         `json:"sourcePolicy"`
	FilteredSources    []string       `json:"filteredSources"`
	SelfHarnessAudits  int            `json:"selfHarnessAudits"`
	ValidationCases    int            `json:"validationCases"`
	EasyAnchorCases    int            `json:"easyAnchorCases"`
	MixedFrontierCases int            `json:"mixedFrontierCases"`
	HardFrontierCases  int            `json:"hardFrontierCases"`
	Opportunities      int            `json:"opportunities"`
}

func EvaluatePropusDoctrineCoverage(
	counts map[string]int,
	validationSummary SkillValidationCaseSummary,
	opportunityCount int,
) (PropusDoctrineCoverage, []string) {
	doctrine := PropusDoctrine()
	covered := make([]string, 0, 4)
	gaps := make([]string, 0, 4)
	actions := make([]string, 0, 4)

	auditEvents := counts["validation_sensitive"]
	evolveEvents := counts["evolved"] + counts["evolve_rejected"] + counts["evolve_rolled_back"]
	axisCoverage := map[string]int{
		"harness_patch":          auditEvents,
		"principle_distillation": counts["genesis"],
		"workflow_topology":      opportunityCount,
	}

	if validationSummary.UniqueRecords > 0 {
		covered = append(covered, "held_out_validation_corpus")
	} else {
		gaps = append(gaps, "missing_held_out_validation_corpus")
		actions = append(actions, "record_validation_case_from_session")
	}
	if auditEvents > 0 {
		covered = append(covered, "self_harness_failure_signature_audit")
	} else if evolveEvents > 0 {
		gaps = append(gaps, "evolve_history_without_self_harness_audit")
		actions = append(actions, "require_failure_signature_audit_before_next_evolve")
	}
	if validationSummary.UniqueMixedFrontierCases > 0 && validationSummary.UniqueEasyAnchorCases > 0 {
		covered = append(covered, "apex_mixed_frontier_with_easy_anchor")
	} else if validationSummary.UniqueRecords >= 2 {
		gaps = append(gaps, "validation_cases_not_tiered_for_apex_frontier")
		actions = append(actions, "tag_validation_cases_easy_mixed_hard")
	} else {
		gaps = append(gaps, "apex_mixed_frontier_unmeasured")
		actions = append(actions, "collect_more_validation_cases_before_large_rewrite")
	}
	if opportunityCount > 0 {
		covered = append(covered, "exploration_backlog_available")
	} else {
		gaps = append(gaps, "exploration_map_empty")
		actions = append(actions, "record_repeated_noop_or_near_miss_as_opportunity")
	}

	state := "unproven"
	if len(gaps) == 0 {
		state = "covered"
	} else if len(covered) > 0 {
		state = "partial"
	}
	return PropusDoctrineCoverage{
		State:              state,
		Covered:            covered,
		Gaps:               gaps,
		AxisCoverage:       axisCoverage,
		SourcePolicy:       "core_sources_only_filtered_sources_not_gates",
		FilteredSources:    doctrine.FilteredSourceIDs(),
		SelfHarnessAudits:  auditEvents,
		ValidationCases:    validationSummary.UniqueRecords,
		EasyAnchorCases:    validationSummary.UniqueEasyAnchorCases,
		MixedFrontierCases: validationSummary.UniqueMixedFrontierCases,
		HardFrontierCases:  validationSummary.UniqueHardFrontierCases,
		Opportunities:      opportunityCount,
	}, actions
}

type PropusOverviewInput struct {
	Scope             string
	SkillName         string
	Recent            []LifecycleLogEntry
	SkillStats        *UsageStats
	Stats             []UsageStats
	Curator           []SkillCuratorRecord
	UsageQuality      UsageQualitySummary
	ValidationSummary SkillValidationCaseSummary
	Opportunities     []SkillOpportunityRecord
	SelfCorrections   []SelfCorrectionCandidateRecord
}

// PropusOverview is the single operational state model. Tool status, native
// UI summaries, and health diagnostics may render different subsets, but should
// not derive their own state machine.
type PropusOverview struct {
	State                  string                 `json:"state"`
	Scope                  string                 `json:"scope"`
	SkillName              string                 `json:"skillName,omitempty"`
	EventCounts            map[string]int         `json:"eventCounts"`
	TrackedSkills          int                    `json:"trackedSkills,omitempty"`
	LowSuccessSkills       int                    `json:"lowSuccessSkills,omitempty"`
	TotalUses              int                    `json:"totalUses,omitempty"`
	SuccessRate            float64                `json:"successRate,omitempty"`
	CountedUsageRecords    int                    `json:"countedUsageRecords"`
	IgnoredUsageRecords    int                    `json:"ignoredUsageRecords"`
	ValidationCases        int                    `json:"validationCases"`
	SkillsWithValidation   int                    `json:"skillsWithValidation,omitempty"`
	PendingSelfCorrections int                    `json:"pendingSelfCorrections"`
	OpenOpportunities      int                    `json:"openOpportunities"`
	CuratedSkills          int                    `json:"curatedSkills,omitempty"`
	StaleSkills            int                    `json:"staleSkills,omitempty"`
	ArchivedSkills         int                    `json:"archivedSkills,omitempty"`
	CuratorState           string                 `json:"curatorState,omitempty"`
	CreatedBy              string                 `json:"createdBy,omitempty"`
	DoctrineCoverage       PropusDoctrineCoverage `json:"doctrineCoverage"`
	NextActions            []string               `json:"nextActions"`
}

func BuildPropusOverview(input PropusOverviewInput) PropusOverview {
	scope := normalizePropusScope(input.Scope)
	counts := PropusLifecycleCounts(input.Recent)
	nextActions := make([]string, 0, 6)
	state := "steady"

	if len(input.SelfCorrections) > 0 {
		state = propusMaxState(state, "needs_review")
		nextActions = append(nextActions, "review_pending_self_corrections")
	}
	if scope == PropusScopeSkill {
		if input.ValidationSummary.UniqueRecords == 0 {
			state = propusMaxState(state, "needs_validation")
			nextActions = append(nextActions, "record_validation_case_from_session")
		}
	} else if input.ValidationSummary.UniqueRecords == 0 && len(input.Stats) > 0 {
		state = propusMaxState(state, "needs_validation")
		nextActions = append(nextActions, "backfill_validation_cases_for_active_skills")
	}

	lowSuccessSkills := 0
	if scope == PropusScopeSkill {
		if input.SkillStats != nil && input.SkillStats.TotalUses >= 2 && input.SkillStats.SuccessRate < 0.5 {
			state = propusMaxState(state, "needs_evolution")
			nextActions = append(nextActions, "inspect_usage_failures_and_propose_evolve")
		}
	} else {
		for _, stat := range input.Stats {
			if stat.TotalUses >= 2 && stat.SuccessRate < 0.5 {
				lowSuccessSkills++
			}
		}
		if lowSuccessSkills > 0 {
			state = propusMaxState(state, "needs_evolution")
			nextActions = append(nextActions, "triage_low_success_skills")
		}
	}

	if counts["evolve_rejected"]+counts["evolve_rolled_back"] > 0 {
		state = propusMaxState(state, "needs_attention")
		if scope == PropusScopeSkill {
			nextActions = append(nextActions, "inspect_rejected_or_rolled_back_edits")
		} else {
			nextActions = append(nextActions, "inspect_recent_rejections_and_rollbacks")
		}
	}
	if len(input.Opportunities) > 0 {
		state = propusMaxState(state, "has_backlog")
		if scope == PropusScopeSkill {
			nextActions = append(nextActions, "triage_opportunity_backlog")
		} else {
			nextActions = append(nextActions, "route_opportunity_backlog")
		}
	}

	doctrineCoverage, doctrineActions := EvaluatePropusDoctrineCoverage(counts, input.ValidationSummary, len(input.Opportunities))
	nextActions = appendUniqueStrings(nextActions, doctrineActions...)
	if len(nextActions) == 0 {
		nextActions = append(nextActions, "continue_observing")
	}
	nextActions = propusPrioritizeNextActions(state, nextActions)

	overview := PropusOverview{
		State:                  state,
		Scope:                  scope,
		SkillName:              strings.TrimSpace(input.SkillName),
		EventCounts:            counts,
		TrackedSkills:          len(input.Stats),
		LowSuccessSkills:       lowSuccessSkills,
		CountedUsageRecords:    input.UsageQuality.CountedRecords,
		IgnoredUsageRecords:    input.UsageQuality.IgnoredRecords,
		ValidationCases:        input.ValidationSummary.UniqueRecords,
		SkillsWithValidation:   input.ValidationSummary.SkillsWithCases,
		PendingSelfCorrections: len(input.SelfCorrections),
		OpenOpportunities:      len(input.Opportunities),
		CuratedSkills:          len(input.Curator),
		DoctrineCoverage:       doctrineCoverage,
		NextActions:            nextActions,
	}
	if input.SkillStats != nil {
		overview.TotalUses = input.SkillStats.TotalUses
		overview.SuccessRate = input.SkillStats.SuccessRate
	}
	if len(input.Curator) > 0 {
		overview.CreatedBy = input.Curator[0].CreatedBy
		overview.CuratorState = input.Curator[0].State
		for _, curator := range input.Curator {
			switch curator.State {
			case SkillCuratorStateStale:
				overview.StaleSkills++
			case SkillCuratorStateArchived:
				overview.ArchivedSkills++
			}
		}
	}
	return overview
}

type PropusLifecycleSummaryInput struct {
	Scope             string
	SkillName         string
	Recent            []LifecycleLogEntry
	Stats             []UsageStats
	Curator           []SkillCuratorRecord
	UsageQuality      UsageQualitySummary
	ValidationSummary SkillValidationCaseSummary
	Opportunities     []SkillOpportunityRecord
	SelfCorrections   []SelfCorrectionCandidateRecord
}

type PropusLifecycleSummary struct {
	System           string                 `json:"system"`
	State            string                 `json:"state"`
	Total            int                    `json:"total"`
	Genesis          int                    `json:"genesis"`
	Evolved          int                    `json:"evolved"`
	Review           int                    `json:"review"`
	Rejected         int                    `json:"rejected"`
	RolledBack       int                    `json:"rolledBack"`
	Attention        int                    `json:"attention"`
	LatestAt         int64                  `json:"latestAt,omitempty"`
	LatestType       string                 `json:"latestType,omitempty"`
	LatestSkill      string                 `json:"latestSkill,omitempty"`
	DoctrineVersion  string                 `json:"doctrineVersion,omitempty"`
	Doctrine         string                 `json:"doctrine,omitempty"`
	SourcePapers     []string               `json:"sourcePapers,omitempty"`
	FilteredSources  []string               `json:"filteredSources,omitempty"`
	Principles       []string               `json:"principles,omitempty"`
	QualityGates     []string               `json:"qualityGates,omitempty"`
	NextActions      []string               `json:"nextActions,omitempty"`
	DoctrineCoverage PropusDoctrineCoverage `json:"doctrineCoverage"`
	NextCue          string                 `json:"nextCue,omitempty"`
	QualityGate      string                 `json:"qualityGate,omitempty"`
	AttentionCue     string                 `json:"attentionCue,omitempty"`
}

func BuildPropusLifecycleSummary(input PropusLifecycleSummaryInput) PropusLifecycleSummary {
	doctrine := PropusDoctrine()
	var skillStats *UsageStats
	if strings.TrimSpace(input.SkillName) != "" {
		for i := range input.Stats {
			if input.Stats[i].SkillName == strings.TrimSpace(input.SkillName) {
				skillStats = &input.Stats[i]
				break
			}
		}
	}
	overview := BuildPropusOverview(PropusOverviewInput{
		Scope:             input.Scope,
		SkillName:         input.SkillName,
		Recent:            input.Recent,
		SkillStats:        skillStats,
		Stats:             input.Stats,
		Curator:           input.Curator,
		UsageQuality:      input.UsageQuality,
		ValidationSummary: input.ValidationSummary,
		Opportunities:     input.Opportunities,
		SelfCorrections:   input.SelfCorrections,
	})
	counts := overview.EventCounts
	summary := PropusLifecycleSummary{
		System:           doctrine.Name,
		State:            overview.State,
		Total:            len(input.Recent),
		Genesis:          counts["genesis"],
		Evolved:          counts["evolved"],
		Review:           counts["review"] + counts["other"],
		Rejected:         counts["evolve_rejected"],
		RolledBack:       counts["evolve_rolled_back"],
		DoctrineVersion:  doctrine.Version,
		Doctrine:         doctrine.LifecycleText(),
		SourcePapers:     doctrine.SourceIDs(),
		FilteredSources:  doctrine.FilteredSourceIDs(),
		Principles:       doctrine.ProductRules(),
		QualityGates:     doctrine.QualityGates,
		NextActions:      overview.NextActions,
		DoctrineCoverage: overview.DoctrineCoverage,
		QualityGate:      "검증 없는 생성/진화는 skill debt로 취급",
		NextCue:          PropusNextCue(overview.State, overview.NextActions),
	}
	summary.Attention = summary.Rejected + summary.RolledBack
	if summary.Total == 0 && overview.State == "steady" {
		summary.State = "idle"
		summary.NextCue = "Propus 활동이 쌓이면 생성/진화/리뷰 압력을 요약합니다"
	}
	if summary.Attention > 0 {
		summary.AttentionCue = "기각/롤백 이벤트를 먼저 열어 같은 실패 후보를 반복하지 마세요"
	}
	for _, entry := range input.Recent {
		if entry.CreatedAt > summary.LatestAt {
			summary.LatestAt = entry.CreatedAt
			summary.LatestType = PropusLifecycleEventType(entry)
			summary.LatestSkill = entry.SkillName
		}
	}
	return summary
}

func PropusLifecycleCounts(entries []LifecycleLogEntry) map[string]int {
	counts := map[string]int{
		"genesis":              0,
		"review":               0,
		"evolved":              0,
		"evolve_rejected":      0,
		"evolve_rolled_back":   0,
		"other":                0,
		"executed_review":      0,
		"non_executed_review":  0,
		"validation_sensitive": 0,
	}
	for _, entry := range entries {
		typ := strings.TrimSpace(entry.Type)
		if typ == "" {
			typ = "genesis"
		}
		switch typ {
		case "genesis", "evolved", "evolve_rejected", "evolve_rolled_back":
			counts[typ]++
		case "evolution_proposal":
			counts["review"]++
			if entry.Executed {
				counts["executed_review"]++
			} else {
				counts["non_executed_review"]++
			}
		default:
			counts["other"]++
		}
		if entry.SelfHarnessAudit != nil {
			counts["validation_sensitive"]++
		}
	}
	return counts
}

func PropusLifecycleEventType(entry LifecycleLogEntry) string {
	switch entry.Type {
	case "genesis", "evolved", "evolve_rejected", "evolve_rolled_back":
		return entry.Type
	default:
		return "review"
	}
}

type PropusHealthInput struct {
	Liveness          SkillLivenessState
	Evolution         EvolutionHealthSummary
	Validation        SkillValidationCaseSummary
	AgentSkills       int
	UnusedAgentSkills int
}

type PropusHealthSnapshot struct {
	State          string   `json:"state"`
	Attention      []string `json:"attention,omitempty"`
	LastActivityMS int64    `json:"lastActivityMS,omitempty"`
}

func BuildPropusHealth(input PropusHealthInput) PropusHealthSnapshot {
	lastActivity := PropusLastActivityMS(input.Liveness)
	attention := propusHealthAttention(input)
	state := "idle"
	if input.Liveness.LastError != "" {
		state = "degraded"
	} else if len(attention) > 0 {
		state = "attention"
	} else if lastActivity > 0 || input.Liveness.ReviewAttempts > 0 || input.Liveness.ReviewSkips > 0 || input.Liveness.ValidationRejections > 0 {
		state = "observing"
	}
	return PropusHealthSnapshot{
		State:          state,
		Attention:      attention,
		LastActivityMS: lastActivity,
	}
}

func PropusLastActivityMS(live SkillLivenessState) int64 {
	last := live.LastReviewAt
	if live.LastEvolveAt > last {
		last = live.LastEvolveAt
	}
	if live.LastGenesisAt > last {
		last = live.LastGenesisAt
	}
	if live.LastErrorAt > last {
		last = live.LastErrorAt
	}
	return last
}

func PropusNextCue(state string, nextActions []string) string {
	if len(nextActions) > 0 {
		switch nextActions[0] {
		case "review_pending_self_corrections":
			return "자가수정 후보를 먼저 리뷰하세요"
		case "record_validation_case_from_session", "backfill_validation_cases_for_active_skills":
			return "검증 케이스를 기록해 생성/진화 debt를 줄이세요"
		case "inspect_usage_failures_and_propose_evolve", "triage_low_success_skills":
			return "낮은 성공률 근거를 확인하고 evolve 후보를 좁히세요"
		case "inspect_rejected_or_rolled_back_edits", "inspect_recent_rejections_and_rollbacks":
			return "기각/롤백 근거 확인"
		case "triage_opportunity_backlog", "route_opportunity_backlog":
			return "opportunity backlog를 route/evolve/no-op로 정리하세요"
		case "tag_validation_cases_easy_mixed_hard":
			return "validation case에 easy/mixed/hard frontier tier를 붙이세요"
		}
	}
	switch state {
	case "idle":
		return "Propus 활동이 쌓이면 생성/진화/리뷰 압력을 요약합니다"
	case "steady":
		return "최근 생성/진화가 검증 근거와 연결되는지 확인"
	default:
		return "Propus overview.nextActions를 우선 처리하세요"
	}
}

func normalizePropusScope(scope string) string {
	if strings.TrimSpace(scope) == PropusScopeSkill {
		return PropusScopeSkill
	}
	return PropusScopeGlobal
}

func propusMaxState(current, candidate string) string {
	if propusStatePriority(candidate) > propusStatePriority(current) {
		return candidate
	}
	return current
}

func propusStatePriority(state string) int {
	switch state {
	case "needs_attention":
		return 5
	case "needs_review":
		return 4
	case "needs_evolution":
		return 3
	case "needs_validation":
		return 2
	case "has_backlog":
		return 1
	default:
		return 0
	}
}

func propusPrioritizeNextActions(state string, actions []string) []string {
	actions = appendUniqueStrings(nil, actions...)
	statePriority := map[string]int{
		"needs_attention":  60,
		"needs_review":     50,
		"needs_evolution":  40,
		"needs_validation": 30,
		"has_backlog":      20,
	}
	sort.SliceStable(actions, func(i, j int) bool {
		left := propusActionPriority(actions[i])
		right := propusActionPriority(actions[j])
		if left == right {
			return false
		}
		// Keep the action matching the selected state first, then preserve the
		// domain priority order for the remaining queue.
		if left == statePriority[state] {
			return true
		}
		if right == statePriority[state] {
			return false
		}
		return left > right
	})
	return actions
}

func propusActionPriority(action string) int {
	switch action {
	case "inspect_rejected_or_rolled_back_edits", "inspect_recent_rejections_and_rollbacks":
		return 60
	case "review_pending_self_corrections":
		return 50
	case "inspect_usage_failures_and_propose_evolve", "triage_low_success_skills":
		return 40
	case "record_validation_case_from_session", "backfill_validation_cases_for_active_skills", "require_failure_signature_audit_before_next_evolve", "tag_validation_cases_easy_mixed_hard", "collect_more_validation_cases_before_large_rewrite":
		return 30
	case "triage_opportunity_backlog", "route_opportunity_backlog", "record_repeated_noop_or_near_miss_as_opportunity":
		return 20
	default:
		return 0
	}
}

func propusHealthAttention(input PropusHealthInput) []string {
	attention := make([]string, 0, 6)
	if input.Liveness.LastError != "" {
		attention = append(attention, "last_error")
	}
	if input.Evolution.Thrash {
		attention = append(attention, "evolve_thrash")
	}
	if input.Evolution.EvolveRolledBack7d > 0 {
		attention = append(attention, "recent_rollbacks")
	}
	if input.Evolution.EvolveRejected7d > 0 {
		attention = append(attention, "recent_rejections")
	}
	if input.Liveness.ReviewAttempts > 0 && input.Liveness.ReviewAttempts == input.Liveness.ReviewSkips && input.Liveness.LastReviewAt == 0 {
		attention = append(attention, "reviews_all_skipped")
	}
	if input.Liveness.ValidationRejections > 0 && input.Validation.UniqueRecords == 0 {
		attention = append(attention, "validation_rejections_without_corpus")
	}
	if input.AgentSkills >= 3 && input.UnusedAgentSkills*2 >= input.AgentSkills {
		attention = append(attention, "many_unused_agent_skills")
	}
	return attention
}

func appendUniqueStrings(base []string, values ...string) []string {
	seen := make(map[string]struct{}, len(base)+len(values))
	out := make([]string, 0, len(base)+len(values))
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	for _, value := range base {
		add(value)
	}
	for _, value := range values {
		add(value)
	}
	return out
}
