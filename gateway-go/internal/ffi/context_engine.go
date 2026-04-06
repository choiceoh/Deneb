package ffi

import (
	"encoding/json"
	"errors"
)

var errContextEngineUnavailable = errors.New("ffi: context engine not available (Rust FFI removed)")

// ContextAssemblyNew is not available (Rust FFI removed).
func ContextAssemblyNew(_, _ uint64, _ uint32) (uint32, error) {
	return 0, errContextEngineUnavailable
}

// ContextAssemblyStart is not available (Rust FFI removed).
func ContextAssemblyStart(_ uint32) (json.RawMessage, error) {
	return nil, errContextEngineUnavailable
}

// ContextAssemblyStep is not available (Rust FFI removed).
func ContextAssemblyStep(_ uint32, _ []byte) (json.RawMessage, error) {
	return nil, errContextEngineUnavailable
}

// ContextExpandNew is not available (Rust FFI removed).
func ContextExpandNew(_ string, _ uint32, _ bool, _ uint64) (uint32, error) {
	return 0, errContextEngineUnavailable
}

// ContextExpandStart is not available (Rust FFI removed).
func ContextExpandStart(_ uint32) (json.RawMessage, error) {
	return nil, errContextEngineUnavailable
}

// ContextExpandStep is not available (Rust FFI removed).
func ContextExpandStep(_ uint32, _ []byte) (json.RawMessage, error) {
	return nil, errContextEngineUnavailable
}

// ContextEngineDrop is a no-op (Rust FFI removed).
func ContextEngineDrop(_ uint32) {}
