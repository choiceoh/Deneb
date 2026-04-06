package ffi

import (
	"context"
	"errors"
)

var errMLUnavailable = errors.New("ffi: ml backend unavailable (Rust FFI removed)")

// MLEmbedCtx is the context-aware embedding variant (returns unavailable).
func MLEmbedCtx(_ context.Context, inputJSON string) ([]byte, error) {
	return MLEmbed(inputJSON)
}

// MLEmbed returns unavailable since Rust FFI was removed.
func MLEmbed(inputJSON string) ([]byte, error) {
	if len(inputJSON) == 0 {
		return nil, errors.New("ffi: ml_embed: empty input")
	}
	return nil, errMLUnavailable
}

// MLAvailable returns false (Rust FFI removed).
func MLAvailable() bool { return false }
