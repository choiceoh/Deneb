//go:build no_ffi || !cgo

package ffi

// MLEmbed is a pure-Go fallback that returns the same stub as the Rust Phase 0 implementation.
func MLEmbed(_ string) ([]byte, error) {
	return []byte(`{"embeddings":[],"phase":0}`), nil
}

// MLRerank is a pure-Go fallback that returns the same stub as the Rust Phase 0 implementation.
func MLRerank(_ string) ([]byte, error) {
	return []byte(`{"ranked":[],"phase":0}`), nil
}
