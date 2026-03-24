//go:build no_ffi || !cgo

package ffi

import (
	"encoding/json"
	"errors"
)

var errContextEngineUnavailable = errors.New("ffi: context engine not available without native FFI")

// ContextAssemblyNew is not available without FFI.
func ContextAssemblyNew(_, _ uint64, _ uint32) (uint32, error) {
	return 0, errContextEngineUnavailable
}

// ContextAssemblyStart is not available without FFI.
func ContextAssemblyStart(_ uint32) (json.RawMessage, error) {
	return nil, errContextEngineUnavailable
}

// ContextAssemblyStep is not available without FFI.
func ContextAssemblyStep(_ uint32, _ []byte) (json.RawMessage, error) {
	return nil, errContextEngineUnavailable
}

// ContextExpandNew is not available without FFI.
func ContextExpandNew(_ string, _ uint32, _ bool, _ uint64) (uint32, error) {
	return 0, errContextEngineUnavailable
}

// ContextExpandStart is not available without FFI.
func ContextExpandStart(_ uint32) (json.RawMessage, error) {
	return nil, errContextEngineUnavailable
}

// ContextExpandStep is not available without FFI.
func ContextExpandStep(_ uint32, _ []byte) (json.RawMessage, error) {
	return nil, errContextEngineUnavailable
}

// ContextEngineDrop is a no-op without FFI.
func ContextEngineDrop(_ uint32) {}
