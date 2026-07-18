package genesis

import "strings"

// Failure-route mode is deliberately shadow-only in the first release: the
// route is exposed to status/sweep consumers and persisted in candidate
// evidence, but it does not change target files, surface tiers, or dispatch
// eligibility. This lets production evidence measure routing quality before a
// wrong classification can send an unattended edit to the wrong subsystem.
const FailureRouteModeShadow = "shadow"

const (
	FailureOriginKnowledge          = "knowledge-gap"
	FailureOriginContextDelivery    = "context-delivery"
	FailureOriginInstruction        = "instruction"
	FailureOriginWorkflow           = "workflow"
	FailureOriginToolRuntime        = "tool-runtime"
	FailureOriginModelCapability    = "model-capability"
	FailureOriginEvaluator          = "evaluator"
	FailureOriginImprovementProcess = "improvement-process"
	FailureOriginUnknown            = "unknown"
)

const (
	InterventionSurfaceMemory           = "memory"
	InterventionSurfaceRetrievalContext = "retrieval-context"
	InterventionSurfaceSkill            = "skill"
	InterventionSurfaceWorkflow         = "workflow"
	InterventionSurfaceToolRuntime      = "tool-runtime"
	InterventionSurfaceModelRole        = "model-role"
	InterventionSurfaceEvaluator        = "evaluator"
	InterventionSurfaceTriage           = "triage"
)

const (
	FailureRouteConfidenceHigh   = "high"
	FailureRouteConfidenceMedium = "medium"
	FailureRouteConfidenceLow    = "low"
)

// FailureInterventionRoute separates where a failure first became observable
// from the cheapest surface likely to fix it. Collapsing both into one
// "failure layer" makes a missing fact, a retrieval miss, and a truncated tool
// result all look like the same context problem even though their fixes differ.
//
// ReasonCodes are stable, machine-readable evidence labels. Alternatives are
// advisory fallbacks for a reviewer; neither field authorizes a mutation.
type FailureInterventionRoute struct {
	Mode                string   `json:"mode"`
	FailureOrigin       string   `json:"failureOrigin"`
	InterventionSurface string   `json:"interventionSurface"`
	Confidence          string   `json:"confidence"`
	ReasonCodes         []string `json:"reasonCodes,omitempty"`
	Alternatives        []string `json:"alternatives,omitempty"`
}

func routeFailureCluster(cluster FailureClusterSummary) FailureInterventionRoute {
	text := normalizedFailureText(strings.Join([]string{
		cluster.Signature,
		cluster.TerminalCause,
		cluster.AgentMechanism,
		cluster.Example,
	}, " "))

	// A verifier/oracle defect must be named explicitly. An ordinary rejection
	// is evidence about a candidate, not proof that the evaluator is wrong.
	if containsAny(
		text,
		"false-reject", "false reject", "grader bug", "verifier bug",
		"test oracle", "oracle bug", "evaluation bug", "test harness bug",
	) {
		return newFailureRoute(
			FailureOriginEvaluator, InterventionSurfaceEvaluator, FailureRouteConfidenceMedium,
			[]string{"explicit_evaluator_defect"}, InterventionSurfaceTriage,
		)
	}

	// Delivery loss wins over generic tool-result wording: the tool may have
	// succeeded perfectly while compaction/clamping hid its result from the
	// model, which is a context intervention rather than a tool repair.
	if containsAny(
		text,
		"truncat", "context-window", "context window", "compaction", "clamp",
		"clipped", "output cut", "tool-result-lost", "tool result lost",
		"missing from context", "omitted from context", "context delivery",
	) {
		return newFailureRoute(
			FailureOriginContextDelivery, InterventionSurfaceRetrievalContext, FailureRouteConfidenceHigh,
			[]string{"explicit_context_delivery_loss"}, InterventionSurfaceToolRuntime,
		)
	}

	// Keep this intentionally narrow. The generic word "missing" is already
	// used for absent artifacts and paths, which must not pollute durable memory.
	if containsAny(
		text,
		"knowledge-gap", "knowledge gap", "missing-fact", "missing fact",
		"memory-miss", "memory miss", "recall-failure", "recall failure",
		"not remembered", "forgotten fact",
	) {
		return newFailureRoute(
			FailureOriginKnowledge, InterventionSurfaceMemory, FailureRouteConfidenceMedium,
			[]string{"explicit_knowledge_or_recall_gap"}, InterventionSurfaceRetrievalContext,
		)
	}

	if containsAny(
		text,
		"tool-error", "tool error", "tool-boundary", "tool boundary",
		"schema-format", "schema or format", "malformed", "invalid argument",
		"invalid-arg", "permission-auth", "permission denied", "unauthorized",
		"authentication", "authorization", "rpc error", "transport error",
		"connection refused", "tool routing", "result recovery",
	) {
		return newFailureRoute(
			FailureOriginToolRuntime, InterventionSurfaceToolRuntime, FailureRouteConfidenceMedium,
			[]string{"tool_boundary_or_contract_signal"}, InterventionSurfaceWorkflow,
		)
	}

	if containsAny(
		text,
		"bounded-execution", "stalled-loop", "retry-discipline", "timeout",
		"timed out", "retry", "no progress", "loop", "wrong order",
		"sequence", "verification", "workflow", "planning",
	) {
		return newFailureRoute(
			FailureOriginWorkflow, InterventionSurfaceWorkflow, FailureRouteConfidenceMedium,
			[]string{"execution_or_sequence_signal"}, InterventionSurfaceSkill,
		)
	}

	if containsAny(
		text,
		"heldout-assertion", "heldout assertion", "skill-behavior-drift",
		"skill behavior drift", "missing-artifact", "artifact-recovery",
		"instruction", "procedure",
	) {
		return newFailureRoute(
			FailureOriginInstruction, InterventionSurfaceSkill, FailureRouteConfidenceMedium,
			[]string{"skill_contract_or_behavior_signal"}, InterventionSurfaceWorkflow,
		)
	}

	// Model routing needs cross-model/cross-surface counterfactual evidence that
	// a single cluster cannot provide. Preserve an explicit capability signal,
	// but keep confidence low and route only to model-role review, never weights.
	if containsAny(
		text,
		"model-capability", "model capability", "capability-limit",
		"capability limit", "reasoning-limit", "reasoning limit",
	) {
		return newFailureRoute(
			FailureOriginModelCapability, InterventionSurfaceModelRole, FailureRouteConfidenceLow,
			[]string{"explicit_capability_signal_without_counterfactual"}, InterventionSurfaceTriage,
		)
	}

	if cluster.Kind == FailureClusterKindRejection {
		switch cluster.Signature {
		case "heldout-replay":
			return newFailureRoute(
				FailureOriginInstruction, InterventionSurfaceSkill, FailureRouteConfidenceMedium,
				[]string{"candidate_behavior_regression"}, InterventionSurfaceWorkflow,
			)
		case "missing-audit", "signature-mismatch", "surface-mismatch", "patch-first":
			return newFailureRoute(
				FailureOriginImprovementProcess, InterventionSurfaceWorkflow, FailureRouteConfidenceMedium,
				[]string{"candidate_generation_or_scoping_rejection"}, InterventionSurfaceTriage,
			)
		default:
			return newFailureRoute(
				FailureOriginImprovementProcess, InterventionSurfaceTriage, FailureRouteConfidenceLow,
				[]string{"unclassified_evolve_rejection"},
			)
		}
	}

	if strings.TrimSpace(cluster.Skill) != "" {
		return newFailureRoute(
			FailureOriginInstruction, InterventionSurfaceSkill, FailureRouteConfidenceLow,
			[]string{"skill_owned_cluster_without_decisive_signal"}, InterventionSurfaceTriage,
		)
	}
	return newFailureRoute(
		FailureOriginUnknown, InterventionSurfaceTriage, FailureRouteConfidenceLow,
		[]string{"insufficient_structured_evidence"},
	)
}

func newFailureRoute(origin, surface, confidence string, reasons []string, alternatives ...string) FailureInterventionRoute {
	return FailureInterventionRoute{
		Mode:                FailureRouteModeShadow,
		FailureOrigin:       origin,
		InterventionSurface: surface,
		Confidence:          confidence,
		ReasonCodes:         cleanFailureRouteValues(reasons),
		Alternatives:        cleanFailureRouteValues(alternatives),
	}
}

func cleanFailureRouteValues(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func formatFailureRouteEvidence(route FailureInterventionRoute) string {
	line := "shadowRoute=origin:" + route.FailureOrigin +
		"; intervention:" + route.InterventionSurface +
		"; confidence:" + route.Confidence
	if len(route.ReasonCodes) > 0 {
		line += "; reasons:" + strings.Join(route.ReasonCodes, ",")
	}
	if len(route.Alternatives) > 0 {
		line += "; alternatives:" + strings.Join(route.Alternatives, ",")
	}
	return line
}
