//go:build no_ffi || !cgo

package ffi

import "errors"

// MLEmbed is a pure-Go fallback when Rust FFI is not linked.
// Returns empty embeddings with an error field.
func MLEmbed(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, errors.New("ffi: ml_embed: empty input")
	}
	return []byte(`{"embeddings":[],"error":"ml backend unavailable (build with CGo + Rust FFI)"}`), nil
}

// MLRerank is a pure-Go fallback when Rust FFI is not linked.
// Returns empty ranked results with an error field.
func MLRerank(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, errors.New("ffi: ml_rerank: empty input")
	}
	return []byte(`{"ranked":[],"error":"ml backend unavailable (build with CGo + Rust FFI)"}`), nil
}
