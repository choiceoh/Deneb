// Package vega implements communication with the Vega project management tool.
//
// The Backend interface abstracts the transport layer.
// RustBackend implements Backend using Rust FFI via deneb-core.
package vega

import (
	"context"
	"encoding/json"
)

// Backend abstracts the Vega execution layer.
type Backend interface {
	// Execute runs a Vega command and returns the JSON result.
	Execute(ctx context.Context, cmd string, args map[string]any) (json.RawMessage, error)
	// Search runs a Vega search query and returns results.
	Search(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error)
	// Close releases resources.
	Close() error
}

// SearchOpts configures a search query.
type SearchOpts struct {
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Mode   string `json:"mode,omitempty"` // "bm25", "semantic", "hybrid"
}

// SearchResult is a single search result.
type SearchResult struct {
	ProjectID   int     `json:"projectId"`
	ProjectName string  `json:"projectName"`
	Section     string  `json:"section,omitempty"`
	Content     string  `json:"content"`
	Score       float64 `json:"score"`
}
