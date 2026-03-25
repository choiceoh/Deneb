package vega

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
)

// RustBackend implements the Backend interface using Rust FFI (deneb-vega).
// This replaces the Python subprocess backend for Vega operations.
type RustBackend struct {
	logger *slog.Logger
}

// RustBackendConfig configures the Rust backend.
type RustBackendConfig struct {
	Logger *slog.Logger
	// DBPath overrides the database path (default: from VEGA env vars).
	DBPath string
	// MDDir overrides the markdown directory.
	MDDir string
}

// NewRustBackend creates a new Rust FFI-based Vega backend.
func NewRustBackend(cfg RustBackendConfig) *RustBackend {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	cfg.Logger.Info("vega: using Rust backend (FFI)")
	return &RustBackend{logger: cfg.Logger}
}

// Execute runs a Vega command via Rust FFI.
func (rb *RustBackend) Execute(ctx context.Context, cmd string, args map[string]any) (json.RawMessage, error) {
	// Build the command JSON expected by deneb_vega_execute:
	// {"command": "search", "args": {...}}
	payload := map[string]any{
		"command": cmd,
		"args":    args,
	}

	cmdJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("vega rust: marshal command: %w", err)
	}

	rb.logger.Debug("vega rust: execute", "command", cmd)
	result, err := ffi.VegaExecute(string(cmdJSON))
	if err != nil {
		return nil, fmt.Errorf("vega rust: ffi execute: %w", err)
	}

	return json.RawMessage(result), nil
}

// Search runs a Vega search query via Rust FFI.
func (rb *RustBackend) Search(ctx context.Context, query string, opts SearchOpts) ([]SearchResult, error) {
	// Build the search JSON expected by deneb_vega_search:
	// {"query": "검색어"}
	payload := map[string]any{
		"query": query,
	}

	queryJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("vega rust: marshal query: %w", err)
	}

	rb.logger.Debug("vega rust: search", "query", query)
	resultBytes, err := ffi.VegaSearch(string(queryJSON))
	if err != nil {
		return nil, fmt.Errorf("vega rust: ffi search: %w", err)
	}

	// Parse the SearchResult from the Rust response.
	// The Rust response is a full SearchResult struct with unified results.
	var rawResult struct {
		Unified []struct {
			ProjectID   int64   `json:"project_id"`
			ProjectName string  `json:"project_name"`
			Heading     string  `json:"heading"`
			Content     string  `json:"content"`
			Score       float64 `json:"score"`
		} `json:"unified"`
		Error  string `json:"error,omitempty"`
		Detail string `json:"detail,omitempty"`
	}

	if err := json.Unmarshal(resultBytes, &rawResult); err != nil {
		return nil, fmt.Errorf("vega rust: unmarshal results: %w", err)
	}

	if rawResult.Error != "" {
		return nil, fmt.Errorf("vega rust: %s: %s", rawResult.Error, rawResult.Detail)
	}

	// Convert to SearchResult slice
	results := make([]SearchResult, 0, len(rawResult.Unified))
	for _, u := range rawResult.Unified {
		results = append(results, SearchResult{
			ProjectID:   int(u.ProjectID),
			ProjectName: u.ProjectName,
			Section:     u.Heading,
			Content:     u.Content,
			Score:       u.Score,
		})
	}

	// Apply limit
	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// Close is a no-op for the Rust backend (no subprocess to terminate).
func (rb *RustBackend) Close() error {
	return nil
}
