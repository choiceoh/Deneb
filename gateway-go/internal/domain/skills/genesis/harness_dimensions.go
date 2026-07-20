package genesis

import "strings"

// MemoHarness-style dimensions describe WHERE the control layer should change.
// They are diagnostic metadata only: validation, adoption, dispatch, and
// rollback continue to be owned by deterministic Deneb gates.
const (
	HarnessDimensionContextAssembly = "D1-context-assembly"
	HarnessDimensionToolInteraction = "D2-tool-interaction"
	HarnessDimensionGeneration      = "D3-generation-control"
	HarnessDimensionOrchestration   = "D4-orchestration"
	HarnessDimensionMemory          = "D5-memory-management"
	HarnessDimensionOutput          = "D6-output-processing"
)

var validHarnessDimensions = map[string]struct{}{
	HarnessDimensionContextAssembly: {},
	HarnessDimensionToolInteraction: {},
	HarnessDimensionGeneration:      {},
	HarnessDimensionOrchestration:   {},
	HarnessDimensionMemory:          {},
	HarnessDimensionOutput:          {},
}

// HarnessDimensionDiagnosis is the bounded six-axis diagnosis attached to a
// failure route. Primary is the cheapest likely control surface; Secondary
// preserves plausible coupled surfaces without letting the classification
// authorize a mutation.
type HarnessDimensionDiagnosis struct {
	Primary   string   `json:"primary"`
	Secondary []string `json:"secondary,omitempty"`
}

func (d HarnessDimensionDiagnosis) empty() bool {
	return normalizeHarnessDimension(d.Primary) == ""
}

func (d HarnessDimensionDiagnosis) ptr() *HarnessDimensionDiagnosis {
	d = newHarnessDimensionDiagnosis(d.Primary, d.Secondary...)
	if d.empty() {
		return nil
	}
	return &d
}

func newHarnessDimensionDiagnosis(primary string, secondary ...string) HarnessDimensionDiagnosis {
	primary = normalizeHarnessDimension(primary)
	if primary == "" {
		return HarnessDimensionDiagnosis{}
	}
	seen := map[string]struct{}{primary: {}}
	out := make([]string, 0, len(secondary))
	for _, dimension := range secondary {
		dimension = normalizeHarnessDimension(dimension)
		if dimension == "" {
			continue
		}
		if _, ok := seen[dimension]; ok {
			continue
		}
		seen[dimension] = struct{}{}
		out = append(out, dimension)
	}
	return HarnessDimensionDiagnosis{Primary: primary, Secondary: out}
}

func normalizeHarnessDimension(value string) string {
	value = strings.TrimSpace(value)
	if _, ok := validHarnessDimensions[value]; ok {
		return value
	}
	return ""
}

func diagnoseHarnessRoute(origin, surface string, reasonCodes []string) HarnessDimensionDiagnosis {
	switch strings.TrimSpace(surface) {
	case InterventionSurfaceMemory:
		return newHarnessDimensionDiagnosis(HarnessDimensionMemory, HarnessDimensionContextAssembly)
	case InterventionSurfaceRetrievalContext:
		secondary := []string(nil)
		if origin == FailureOriginKnowledge {
			secondary = append(secondary, HarnessDimensionMemory)
		}
		return newHarnessDimensionDiagnosis(HarnessDimensionContextAssembly, secondary...)
	case InterventionSurfaceSkill:
		return newHarnessDimensionDiagnosis(HarnessDimensionContextAssembly, HarnessDimensionOrchestration)
	case InterventionSurfaceWorkflow:
		return newHarnessDimensionDiagnosis(HarnessDimensionOrchestration)
	case InterventionSurfaceToolRuntime:
		secondary := []string(nil)
		if containsHarnessReason(reasonCodes, "tool_boundary_or_contract_signal") {
			secondary = append(secondary, HarnessDimensionOutput)
		}
		return newHarnessDimensionDiagnosis(HarnessDimensionToolInteraction, secondary...)
	case InterventionSurfaceModelRole:
		return newHarnessDimensionDiagnosis(HarnessDimensionGeneration)
	case InterventionSurfaceEvaluator:
		return newHarnessDimensionDiagnosis(HarnessDimensionOutput)
	case InterventionSurfaceTriage:
		if origin == FailureOriginImprovementProcess {
			return newHarnessDimensionDiagnosis(HarnessDimensionOrchestration)
		}
	}
	return HarnessDimensionDiagnosis{}
}

func containsHarnessReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if strings.TrimSpace(reason) == want {
			return true
		}
	}
	return false
}

// withHarnessDimensions enriches legacy and freshly generated audits without
// trusting a model-provided dimension. Failure evidence chooses the primary
// axis; the concrete edited SKILL.md section is retained as a secondary axis
// when it represents a different control surface.
func withHarnessDimensions(audit HarnessEditAudit) HarnessEditAudit {
	existing := newHarnessDimensionDiagnosis(audit.PrimaryDimension, audit.SecondaryDimensions...)
	diagnosis := existing
	if strings.TrimSpace(audit.TargetSignature) != "" {
		route := routeFailureCluster(FailureClusterSummary{
			Kind:      FailureClusterKindUsage,
			Skill:     "skill-body",
			Signature: audit.TargetSignature,
		})
		if route.HarnessDiagnosis != nil {
			diagnosis = *route.HarnessDiagnosis
		}
	}
	if edited := harnessDimensionForEditedSurface(audit.EditedSurface); edited != "" {
		if diagnosis.empty() {
			diagnosis = newHarnessDimensionDiagnosis(edited)
		} else {
			diagnosis = newHarnessDimensionDiagnosis(
				diagnosis.Primary,
				append(diagnosis.Secondary, edited)...,
			)
		}
	}
	audit.PrimaryDimension = diagnosis.Primary
	audit.SecondaryDimensions = diagnosis.Secondary
	return audit
}

func harnessDimensionForEditedSurface(surface string) string {
	surface = strings.ToLower(strings.TrimSpace(surface))
	switch {
	case containsAny(surface, "verification", "output", "response", "result", "schema", "format"):
		return HarnessDimensionOutput
	case containsAny(surface, "procedure", "workflow", "orchestration", "steps", "plan"):
		return HarnessDimensionOrchestration
	case containsAny(surface, "tool"):
		return HarnessDimensionToolInteraction
	case containsAny(surface, "memory", "state", "retention"):
		return HarnessDimensionMemory
	case containsAny(surface, "model", "generation", "decoding"):
		return HarnessDimensionGeneration
	case surface != "":
		return HarnessDimensionContextAssembly
	default:
		return ""
	}
}

func harnessDiagnosisForFailurePattern(signature, terminalCause, mechanism string) *HarnessDimensionDiagnosis {
	route := routeFailureCluster(FailureClusterSummary{
		Kind:           FailureClusterKindUsage,
		Skill:          "skill-body",
		Signature:      signature,
		TerminalCause:  terminalCause,
		AgentMechanism: mechanism,
	})
	return route.HarnessDiagnosis
}

func harnessDimensionsForSignatures(signatures []string) []string {
	seen := make(map[string]struct{}, len(signatures))
	out := make([]string, 0, len(signatures))
	for _, signature := range signatures {
		diagnosis := harnessDiagnosisForFailurePattern(signature, "", "")
		if diagnosis == nil {
			continue
		}
		for _, dimension := range append([]string{diagnosis.Primary}, diagnosis.Secondary...) {
			if dimension = normalizeHarnessDimension(dimension); dimension == "" {
				continue
			}
			if _, ok := seen[dimension]; ok {
				continue
			}
			seen[dimension] = struct{}{}
			out = append(out, dimension)
		}
	}
	return out
}

func formatHarnessDiagnosis(diagnosis *HarnessDimensionDiagnosis) string {
	if diagnosis == nil || diagnosis.empty() {
		return ""
	}
	line := diagnosis.Primary
	if len(diagnosis.Secondary) > 0 {
		line += " (secondary: " + strings.Join(diagnosis.Secondary, ", ") + ")"
	}
	return line
}
