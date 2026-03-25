//go:build no_ffi || !cgo

package ffi

// VegaExecute is a pure-Go fallback that returns the same stub as the Rust Phase 0 implementation.
func VegaExecute(_ string) ([]byte, error) {
	return []byte(`{"error":"vega_not_implemented","phase":0}`), nil
}

// VegaSearch is a pure-Go fallback that returns the same stub as the Rust Phase 0 implementation.
func VegaSearch(_ string) ([]byte, error) {
	return []byte(`{"results":[],"phase":0}`), nil
}
