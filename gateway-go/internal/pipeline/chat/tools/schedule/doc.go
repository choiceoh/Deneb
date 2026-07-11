// Package schedule owns the calendar tool's merged read model, formatting,
// availability analysis, work-graph queries, and local-event mutations.
//
// It depends only on calendar contracts and never imports its parent tools
// package; registration and structured-tool callers compose its public API.
package schedule
