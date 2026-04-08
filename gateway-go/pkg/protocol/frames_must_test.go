package protocol

import (
	"math"
	"testing"
)



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
		t.Errorf("got %s, want code %s", resp.Error.Code, ErrUnavailable)
	}
}

