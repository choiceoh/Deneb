// Package status defines the stable read model for recursive self-improvement
// health reporting. Keeping this projection separate from the mutation-heavy
// genesis engine gives RPC consumers a narrow dependency boundary.
package status

const (
	StateLive      = "LIVE"
	StateDataGated = "DATA-GATED"
	StateStarved   = "STARVED"
	StateFrozen    = "FROZEN"
	StateIdle      = "IDLE"
)

// LoopStatus is the complete recursive-self-improvement snapshot.
type LoopStatus struct {
	Layers  []Layer
	Turning int
	Health  Health
}

// Health contains numeric evolution-health signals for client scoreboards.
type Health struct {
	Evolves7d         int
	Confirmed7d       int
	Rejected7d        int
	RolledBack7d      int
	Genesis7d         int
	ConfirmRate       float64
	FalseAcceptRate   float64
	ResolvedEvolves7d int
	Thrash            bool
	AutoAdoptFrozen   bool
	MetaRevisions7d   int
}

// Layer is one loop layer's classified state and display metrics.
type Layer struct {
	Key       string
	Title     string
	State     string
	Diagnosis string
	Detail    string
	Metrics   []Metric
}

// Metric is one preformatted display value.
type Metric struct {
	Label string
	Value string
}
