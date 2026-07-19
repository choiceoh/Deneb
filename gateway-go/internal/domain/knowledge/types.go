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

// Writer extends Adapter for backends that accept agent-initiated writes.
// Only the wiki adapter implements this today.
type Writer interface {
	Adapter
	Record(ctx context.Context, opts RecordOptions) (Ref, error)
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
