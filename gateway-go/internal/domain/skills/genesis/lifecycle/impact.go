package lifecycle

// ImpactContract declares how a safely delivered correction will be judged
// for usefulness after its observation window.
type ImpactContract struct {
	Metric              string   `json:"metric"`
	Direction           string   `json:"direction"`
	Baseline            float64  `json:"baseline"`
	Target              float64  `json:"target"`
	MinSamples          int      `json:"minSamples"`
	ObservationWindowMs int64    `json:"observationWindowMs,omitempty"`
	Guardrails          []string `json:"guardrails,omitempty"`
}

// ImpactResult is one terminal usefulness verdict for an exact delivery
// attempt. A pending verdict remains derived and is never persisted.
type ImpactResult struct {
	Status              string   `json:"status"`
	Observed            float64  `json:"observed"`
	Samples             int      `json:"samples"`
	GuardrailViolations []string `json:"guardrailViolations,omitempty"`
	Note                string   `json:"note,omitempty"`
	CheckedAt           int64    `json:"checkedAt"`
}
