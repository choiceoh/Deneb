//go:build no_ffi || !cgo

package ffi

import "errors"

var errVegaUnavailable = errors.New("ffi: vega backend unavailable (build with CGo + Rust FFI)")

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
