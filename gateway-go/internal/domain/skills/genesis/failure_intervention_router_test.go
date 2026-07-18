package genesis

import (
	"reflect"
	"testing"
	"time"
)

func TestRouteFailureClusterSeparatesOriginFromIntervention(t *testing.T) {
	tests := []struct {
		name       string
		cluster    FailureClusterSummary
		origin     string
		surface    string
		confidence string
		reason     string
	}{
		{
			name: "context delivery loss is not a tool repair",
			cluster: FailureClusterSummary{
				Skill: "log-reader", Signature: "terminal=insufficient-context|mechanism=tool-result-truncation",
				Example: "tool output was truncated before the model saw the answer",
			},
			origin: FailureOriginContextDelivery, surface: InterventionSurfaceRetrievalContext,
			confidence: FailureRouteConfidenceHigh, reason: "explicit_context_delivery_loss",
		},
		{
			name: "narrow fact miss routes to memory",
			cluster: FailureClusterSummary{
				Skill: "account-brief", Signature: "terminal=missing-artifact|mechanism=artifact-recovery",
				Example: "missing fact: customer's preferred billing cycle was not remembered",
			},
			origin: FailureOriginKnowledge, surface: InterventionSurfaceMemory,
			confidence: FailureRouteConfidenceMedium, reason: "explicit_knowledge_or_recall_gap",
		},
		{
			name: "tool contract",
			cluster: FailureClusterSummary{
				Skill: "mail", Signature: "terminal=schema-format|mechanism=structured-contract",
			},
			origin: FailureOriginToolRuntime, surface: InterventionSurfaceToolRuntime,
			confidence: FailureRouteConfidenceMedium, reason: "tool_boundary_or_contract_signal",
		},
		{
			name: "workflow timeout",
			cluster: FailureClusterSummary{
				Skill: "deploy", Signature: "terminal=timeout|mechanism=bounded-execution",
			},
			origin: FailureOriginWorkflow, surface: InterventionSurfaceWorkflow,
			confidence: FailureRouteConfidenceMedium, reason: "execution_or_sequence_signal",
		},
		{
			name: "skill behavior drift",
			cluster: FailureClusterSummary{
				Kind: FailureClusterKindWorkout, Skill: "contract-review",
				Signature: "terminal=heldout-assertion|mechanism=skill-behavior-drift",
			},
			origin: FailureOriginInstruction, surface: InterventionSurfaceSkill,
			confidence: FailureRouteConfidenceMedium, reason: "skill_contract_or_behavior_signal",
		},
		{
			name: "explicit evaluator defect remains advisory",
			cluster: FailureClusterSummary{
				Kind: FailureClusterKindRejection, Skill: "judge", Signature: "other",
				Example: "false reject caused by verifier bug",
			},
			origin: FailureOriginEvaluator, surface: InterventionSurfaceEvaluator,
			confidence: FailureRouteConfidenceMedium, reason: "explicit_evaluator_defect",
		},
		{
			name: "ordinary rejection is improvement workflow evidence",
			cluster: FailureClusterSummary{
				Kind: FailureClusterKindRejection, Skill: "judge", Signature: "surface-mismatch",
			},
			origin: FailureOriginImprovementProcess, surface: InterventionSurfaceWorkflow,
			confidence: FailureRouteConfidenceMedium, reason: "candidate_generation_or_scoping_rejection",
		},
		{
			name: "model signal cannot authorize weight changes",
			cluster: FailureClusterSummary{
				Skill: "reasoner", Model: "small-local", Signature: "terminal=model-capability|mechanism=reasoning-limit",
			},
			origin: FailureOriginModelCapability, surface: InterventionSurfaceModelRole,
			confidence: FailureRouteConfidenceLow, reason: "explicit_capability_signal_without_counterfactual",
		},
		{
			name: "unknown global failure goes to triage",
			cluster: FailureClusterSummary{
				Signature: "terminal=other|mechanism=opaque",
			},
			origin: FailureOriginUnknown, surface: InterventionSurfaceTriage,
			confidence: FailureRouteConfidenceLow, reason: "insufficient_structured_evidence",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := routeFailureCluster(test.cluster)
			if route.Mode != FailureRouteModeShadow || route.FailureOrigin != test.origin ||
				route.InterventionSurface != test.surface || route.Confidence != test.confidence {
				t.Fatalf("route = %+v, want mode=%s origin=%s surface=%s confidence=%s",
					route, FailureRouteModeShadow, test.origin, test.surface, test.confidence)
			}
			if !reflect.DeepEqual(route.ReasonCodes, []string{test.reason}) {
				t.Fatalf("reason codes = %v, want [%s]", route.ReasonCodes, test.reason)
			}
		})
	}
}

func TestFailureEvidenceClustersAttachShadowRoutes(t *testing.T) {
	tracker := newTestTracker(t)
	now := int64(1_783_500_000_000)
	if err := tracker.RecordUsage(UsageRecord{
		SkillName: "log-reader", SessionKey: "client:main", Source: UsageSourceReal,
		Success: false, UsedAt: now, ErrorMsg: "tool output was truncated before the model saw the answer",
		FailureTrace: &UsageFailureTrace{
			Signature:      "terminal=insufficient-context|mechanism=tool-result-truncation",
			TerminalCause:  "tool output truncated",
			AgentMechanism: "context delivery loss",
		},
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}

	clusters := failureClustersForTest(tracker, time.UnixMilli(now), 10)
	if len(clusters) != 1 {
		t.Fatalf("clusters = %+v, want one", clusters)
	}
	if route := clusters[0].Route; route.Mode != FailureRouteModeShadow ||
		route.FailureOrigin != FailureOriginContextDelivery ||
		route.InterventionSurface != InterventionSurfaceRetrievalContext {
		t.Fatalf("cluster shadow route = %+v", route)
	}
}
