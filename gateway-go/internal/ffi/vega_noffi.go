//go:build no_ffi || !cgo

package ffi

import (
	"context"
	"errors"
)

var errVegaUnavailable = errors.New("ffi: vega backend unavailable (build with CGo + Rust FFI)")

// VegaExecuteCtx is the context-aware variant (noffi delegates to the non-ctx version).
func VegaExecuteCtx(ctx context.Context, cmd string) ([]byte, error) {
	return VegaExecute(cmd)
}

// VegaSearchCtx is the context-aware variant (noffi delegates to the non-ctx version).
func VegaSearchCtx(ctx context.Context, query string) ([]byte, error) {
	return VegaSearch(query)
}

// VegaExecute is a pure-Go fallback when Rust FFI is not linked.
func VegaExecute(cmd string) ([]byte, error) {
	if len(cmd) == 0 {
		return nil, errors.New("ffi: vega_execute: empty input")
	}
	return nil, errVegaUnavailable
}

// VegaSearch is a pure-Go fallback when Rust FFI is not linked.
func VegaSearch(query string) ([]byte, error) {
	if len(query) == 0 {
		return nil, errors.New("ffi: vega_search: empty input")
	}
	return nil, errVegaUnavailable
}
