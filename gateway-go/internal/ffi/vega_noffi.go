//go:build no_ffi || !cgo

package ffi

import "errors"

// VegaExecute is a pure-Go fallback when Rust FFI is not linked.
// Returns a structured error indicating the backend is unavailable.
func VegaExecute(cmd string) ([]byte, error) {
	if len(cmd) == 0 {
		return nil, errors.New("ffi: vega_execute: empty input")
	}
	return []byte(`{"error":"vega backend unavailable (build with CGo + Rust FFI)"}`), nil
}

// VegaSearch is a pure-Go fallback when Rust FFI is not linked.
// Returns empty results with an error field.
func VegaSearch(query string) ([]byte, error) {
	if len(query) == 0 {
		return nil, errors.New("ffi: vega_search: empty input")
	}
	return []byte(`{"results":[],"error":"vega backend unavailable (build with CGo + Rust FFI)"}`), nil
}
