package handlerminiapp

// RSILoopStatusResponse is the miniapp.rsi.status payload: the four loop layers
// (L1 skill evolution, L2 meta-evolution, L3 verifier co-evolution, L4 source
// self-edit) each with an honest state and display metrics, plus how many are
// turning right now. Behavior lives in the skillsrpc subpackage; this file
// stays the client generator's source of truth for the //deneb:wire types.
//
//deneb:wire
type RSILoopStatusResponse struct {
	Layers  []RSILayerView `json:"layers"`
	Turning int            `json:"turning"`
	Health  RSIHealthView  `json:"health"`
}

// RSIHealthView is the structured evolution-health scoreboard (numeric fields
// behind the layer diagnoses) so clients can draw a real scoreboard instead of
// parsing preformatted metric strings.
//
//deneb:wire
type RSIHealthView struct {
	Evolves7d         int     `json:"evolves7d"`
	Confirmed7d       int     `json:"confirmed7d"`
	Rejected7d        int     `json:"rejected7d"`
	RolledBack7d      int     `json:"rolledBack7d"`
	Genesis7d         int     `json:"genesis7d"`
	ConfirmRate       float64 `json:"confirmRate"`
	FalseAcceptRate   float64 `json:"falseAcceptRate"`
	ResolvedEvolves7d int     `json:"resolvedEvolves7d"`
	Thrash            bool    `json:"thrash"`
	AutoAdoptFrozen   bool    `json:"autoAdoptFrozen"`
	MetaRevisions7d   int     `json:"metaRevisions7d"`
}

// RSILayerView is one loop layer's classified state. State is one of LIVE,
// DATA-GATED, STARVED, FROZEN, IDLE.
//
//deneb:wire
type RSILayerView struct {
	Key       string          `json:"key"`
	Title     string          `json:"title"`
	State     string          `json:"state"`
	Diagnosis string          `json:"diagnosis"`
	Detail    string          `json:"detail,omitempty"`
	Metrics   []RSIMetricView `json:"metrics,omitempty"`
}

// RSIMetricView is one display metric (label + preformatted value).
//
//deneb:wire
type RSIMetricView struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
