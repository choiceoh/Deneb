package vega

import (
	"testing"
)

func TestShouldEnableVega_NoFFI(t *testing.T) {
	got := ShouldEnableVega(false, "", nil)
	if got {
		t.Error("should return false when FFI is not available")
	}
}

func TestShouldEnableVega_FFIAvailable(t *testing.T) {
	got := ShouldEnableVega(true, "", nil)
	if !got {
		t.Error("should return true when FFI available")
	}
}

func TestIsLocalAIReachable_EmptyURL(t *testing.T) {
	got := IsLocalAIReachable("")
	if got {
		t.Error("should return false for empty URL")
	}
}

func TestIsLocalAIReachable_InvalidURL(t *testing.T) {
	got := IsLocalAIReachable("http://127.0.0.1:99999/v1")
	if got {
		t.Error("should return false for unreachable URL")
	}
}
