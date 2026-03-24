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

func TestValidateSessionKey(t *testing.T) {
	if err := ValidateSessionKey("my-session-123"); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
	if err := ValidateSessionKey("a"); err != nil {
		t.Errorf("expected valid single char, got: %v", err)
	}
	if err := ValidateSessionKey(""); err == nil {
		t.Error("expected error for empty key")
	}
	if err := ValidateSessionKey("has\x00null"); err == nil {
		t.Error("expected error for control char in key")
	}
}

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{"a & b", "a &amp; b"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"it's", "it&#x27;s"},
		{"", ""},
	}
	for _, tt := range tests {
		got := SanitizeHTML(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeHTML(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsSafeURL(t *testing.T) {
	safe := []string{
		"https://example.com/api",
		"http://cdn.example.com/image.png",
	}
	for _, url := range safe {
		if !IsSafeURL(url) {
			t.Errorf("expected safe: %s", url)
		}
	}
	blocked := []string{
		"http://localhost/admin",
		"http://127.0.0.1:8080/",
		"http://10.0.0.1/secret",
		"http://192.168.1.1/",
		"http://169.254.169.254/latest/meta-data/",
		"ftp://example.com/file",
		"",
	}
	for _, url := range blocked {
		if IsSafeURL(url) {
			t.Errorf("expected blocked: %s", url)
		}
	}
}

func TestValidateErrorCode(t *testing.T) {
	valid := []string{
		"NOT_LINKED", "NOT_PAIRED", "AGENT_TIMEOUT", "INVALID_REQUEST",
		"UNAVAILABLE", "MISSING_PARAM", "NOT_FOUND", "UNAUTHORIZED",
		"VALIDATION_FAILED", "CONFLICT", "FORBIDDEN", "NODE_DISCONNECTED",
		"DEPENDENCY_FAILED", "FEATURE_DISABLED",
	}
	for _, code := range valid {
		if !ValidateErrorCode(code) {
			t.Errorf("expected valid error code: %s", code)
		}
	}
	if ValidateErrorCode("BOGUS") {
		t.Error("expected invalid for unknown code")
	}
	if ValidateErrorCode("") {
		t.Error("expected invalid for empty code")
	}
}

func TestAvailable(t *testing.T) {
	t.Logf("FFI available: %v", Available)
}
