package knowledge

import "context"

// Result is a single hit returned by Recall.
type Result struct {
	Ref        Ref
	Snippet    string
	Context    string // bounded late-expanded context; Snippet remains the precise match
	Score      float64
	Provenance Provenance
	Meta       map[string]string // legacy/backend-specific fields not yet promoted into Provenance
	Time       int64             // unix milli, 0 when the backend does not surface a timestamp
}

// EvidencePacket is the typed boundary between source planning/retrieval and
// agent-facing synthesis. It keeps the plan, degradation notes, provenance,
// freshness, exact match, and late-expanded context together instead of
// flattening those distinctions into one display string.
type EvidencePacket struct {
	Query       string
	Plan        RecallPlan
	Results     []Result
	Notes       []string
	RetrievedAt int64 // unix milli
}

// Document is the full content of one knowledge entry fetched by Read.
type Document struct {
	Ref     Ref
	Title   string // optional, e.g. wiki page title; empty for opaque memories
	Content string
	Meta    map[string]string // backend-specific fields surfaced verbatim
	Time    int64
}

// Adapter is the read-side interface every knowledge backend must implement.
type Adapter interface {
	Layer() Layer
	Recall(ctx context.Context, query string, limit int) ([]Result, error)
	Read(ctx context.Context, id string) (*Document, error)
}

// RecallResultGuard is an optional post-federation safety boundary. The wiki
// fact plane implements it so corrections and tombstones also govern evidence
// returned by other adapters (for example an older value in a source file).
type RecallResultGuard interface {
	FilterRecallResults(results []Result) []Result
}

// Writer extends Adapter for backends that accept agent-initiated writes.
// Only the wiki adapter implements this today.
type Writer interface {
	Adapter
	Record(ctx context.Context, opts RecordOptions) (Ref, error)
}

// FactWriter is the typed current-state extension implemented by the wiki
// backend. It keeps fact correction/history behind the same knowledge surface
// instead of adding another agent tool.
type FactWriter interface {
	Adapter
	RecordFact(ctx context.Context, opts FactRecordOptions) (FactMutationResult, error)
	ForgetFact(ctx context.Context, opts FactForgetOptions) (FactMutationResult, error)
	// Facts returns one key's full history when key is non-empty. With an
	// empty key it returns active facts for subject; an empty subject defaults
	// to self so an agent-facing query cannot accidentally enumerate others.
	Facts(ctx context.Context, subject, key string) ([]FactView, error)
}

// RecordOptions carries the fields the wiki record path needs. Optional
// fields are zero-valued when the caller does not supply them.
type RecordOptions struct {
	// Page is the wiki path (e.g. "인물/박부장"). Required.
	Page string
	// Title overrides page Meta.Title; defaults to the last path segment.
	Title string
	// Category groups the page (e.g. "프로젝트", "인물", "시스템").
	Category string
	// Body is the markdown content.
	Body string
	// Tags / Related populate the page frontmatter.
	Tags       []string
	Related    []string
	Supersedes []string
	// Summary is the one-line index-level description.
	Summary string
	// Importance 0.0–1.0 for Tier1 surfacing.
	Importance float64
}

type FactRecordOptions struct {
	Subject   string
	Key       string
	Value     string
	Kind      string
	Authority string
	Sources   []string
	BasisAt   string
	Actor     string
	Reason    string
}

type FactForgetOptions struct {
	Subject   string
	Key       string
	Authority string
	Sources   []string
	Actor     string
	Reason    string
}

type FactMutationResult struct {
	Revision        uint64
	ClaimID         string
	Status          string
	Resolution      string
	Committed       bool
	ProjectionError string
}

// FactMutationObserver runs synchronously after a canonical fact assertion or
// tombstone commits. Runtime wiring uses it to invalidate session-frozen
// context and retrieval caches before the mutating tool call returns.
type FactMutationObserver func(FactMutationResult)

type FactView struct {
	ID           string   `json:"id"`
	Subject      string   `json:"subject"`
	Key          string   `json:"key"`
	Value        string   `json:"value"`
	Kind         string   `json:"kind"`
	Authority    string   `json:"authority"`
	Status       string   `json:"status"`
	Sources      []string `json:"sources,omitempty"`
	Actor        string   `json:"actor,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	RecordedAtMs int64    `json:"recordedAtMs"`
	BasisAtMs    int64    `json:"basisAtMs,omitempty"`
	Revision     uint64   `json:"revision"`
}
