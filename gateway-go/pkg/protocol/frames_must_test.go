package protocol

import (
	"math"
	"testing"
)

func TestMustResponseOK_Success(t *testing.T) {
	resp := MustResponseOK("test-1", map[string]string{"status": "ok"})
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}
	if resp.ID != "test-1" {
		t.Errorf("expected ID=test-1, got %s", resp.ID)
	}
}

func TestMustResponseOK_NilPayload(t *testing.T) {
	resp := MustResponseOK("test-2", nil)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.OK {
		t.Fatal("expected OK=true for nil payload")
	}
}

func TestMustResponseOK_MarshalFail(t *testing.T) {
	// math.NaN() causes json.Marshal to fail.
	resp := MustResponseOK("test-3", math.NaN())
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.OK {
		t.Fatal("expected OK=false for unmarshalable payload")
	}
	if resp.Error == nil {
		t.Fatal("expected error shape")
	}
	if resp.Error.Code != ErrUnavailable {
		t.Errorf("expected code %s, got %s", ErrUnavailable, resp.Error.Code)
	}
}

func TestMustResponseOKRaw_Success(t *testing.T) {
	resp := MustResponseOKRaw("test-4", []byte(`{"ok":true}`))
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.OK {
		t.Fatal("expected OK=true")
	}
}
