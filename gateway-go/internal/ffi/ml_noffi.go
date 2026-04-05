//go:build no_ffi || !cgo

package ffi

import (
	"context"
	"errors"
)

var errMLUnavailable = errors.New("ffi: ml backend unavailable (build with CGo + Rust FFI)")

// MLEmbedCtx is the context-aware variant (noffi delegates to the non-ctx version).
func MLEmbedCtx(ctx context.Context, inputJSON string) ([]byte, error) {
	return MLEmbed(inputJSON)
}

// MLEmbed is a pure-Go fallback when Rust FFI is not linked.
func MLEmbed(inputJSON string) ([]byte, error) {
	if len(inputJSON) == 0 {
		return nil, errors.New("ffi: ml_embed: empty input")
	}
	return nil, errMLUnavailable
}
