//go:build no_ffi || !cgo

package ffi

import "errors"

// VegaExecute is a pure-Go fallback that returns the same stub as the Rust Phase 0 implementation.
func VegaExecute(cmd string) ([]byte, error) {
	if len(cmd) == 0 {
		return nil, errors.New("ffi: vega_execute: empty input")
	}
	return []byte(`{"error":"vega_not_implemented","phase":0}`), nil
}

// VegaSearch is a pure-Go fallback that returns the same stub as the Rust Phase 0 implementation.
func VegaSearch(query string) ([]byte, error) {
	if len(query) == 0 {
		return nil, errors.New("ffi: vega_search: empty input")
	}
	return []byte(`{"results":[],"phase":0}`), nil
}
