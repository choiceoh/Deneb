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

func TestIsSglangReachable_EmptyURL(t *testing.T) {
	got := IsSglangReachable("")
	if got {
		t.Error("should return false for empty URL")
	}
}

func TestIsSglangReachable_InvalidURL(t *testing.T) {
	got := IsSglangReachable("http://127.0.0.1:99999/v1")
	if got {
		t.Error("should return false for unreachable URL")
	}
}
