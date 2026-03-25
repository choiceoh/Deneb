//go:build no_ffi || !cgo

package ffi

import "errors"

// MLEmbed is a pure-Go fallback that returns the same stub as the Rust Phase 0 implementation.
func MLEmbed(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, errors.New("ffi: ml_embed: empty input")
	}
	return []byte(`{"embeddings":[],"phase":0}`), nil
}

// MLRerank is a pure-Go fallback that returns the same stub as the Rust Phase 0 implementation.
func MLRerank(input string) ([]byte, error) {
	if len(input) == 0 {
		return nil, errors.New("ffi: ml_rerank: empty input")
	}
	return []byte(`{"ranked":[],"phase":0}`), nil
}
