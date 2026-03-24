package ffi

import "testing"

func TestValidateFrame_ValidRequest(t *testing.T) {
	if err := ValidateFrame(`{"type":"req","id":"1","method":"chat.send"}`); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateFrame_ValidResponse(t *testing.T) {
	if err := ValidateFrame(`{"type":"res","id":"1","ok":true}`); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateFrame_ValidEvent(t *testing.T) {
	if err := ValidateFrame(`{"type":"event","event":"channel.connected"}`); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateFrame_Invalid(t *testing.T) {
	if err := ValidateFrame(`{"type":"unknown"}`); err == nil {
		t.Error("expected error for unknown frame type")
	}
}

func TestValidateFrame_Empty(t *testing.T) {
	if err := ValidateFrame(""); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestConstantTimeEq(t *testing.T) {
	if !ConstantTimeEq([]byte("secret"), []byte("secret")) {
		t.Error("expected equal")
	}
	if ConstantTimeEq([]byte("secret"), []byte("differ")) {
		t.Error("expected not equal")
	}
	if ConstantTimeEq([]byte("short"), []byte("longer string")) {
		t.Error("expected not equal for different lengths")
	}
	if !ConstantTimeEq([]byte{}, []byte{}) {
		t.Error("expected equal for empty slices")
	}
}

func TestDetectMIME(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"PNG", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"PDF", []byte("%PDF-1.4"), "application/pdf"},
		{"unknown", []byte{0x00, 0x01, 0x02, 0x03, 0x04}, "application/octet-stream"},
		{"empty", nil, "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectMIME(tt.data); got != tt.want {
				t.Errorf("DetectMIME() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAvailable(t *testing.T) {
	t.Logf("FFI available: %v", Available)
}
