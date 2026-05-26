package knowledge

import "context"

// Result is a single hit returned by Recall.
type Result struct {
	Ref     Ref
	Snippet string
	Score   float64
	Time    int64 // unix milli, 0 when the backend does not surface a timestamp
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
// Only the wiki adapter implements this — hindsight retains automatically
// from completed turns via hindsight_recorder.
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
	// Category groups the page (e.g. "인물", "거래", "프로젝트", "기술").
	Category string
	// Body is the markdown content.
	Body string
	// Tags / Related populate the page frontmatter.
	Tags    []string
	Related []string
	// Summary is the one-line index-level description.
	Summary string
	// Importance 0.0–1.0 for Tier1 surfacing.
	Importance float64
}
