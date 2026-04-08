package ffi

import (
	"testing"
)

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

func TestIsSafeURL_FileAndUNC(t *testing.T) {
	blocked := []string{
		"file:///etc/passwd",
		"FILE:///etc/passwd",
		"file://localhost/etc/passwd",
		"file:///C:/Windows/System32",
		"\\\\server\\share",
		"//server/share",
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("expected %q to be blocked", u)
		}
	}
	// Ensure valid URLs still pass
	allowed := []string{
		"https://example.com",
		"http://example.com/path",
	}
	for _, u := range allowed {
		if !IsSafeURL(u) {
			t.Errorf("expected %q to be allowed", u)
		}
	}
}

func TestIsSafeURL_CGNAT(t *testing.T) {
	blocked := []string{
		"http://100.64.0.1/",
		"http://100.100.100.100/",
		"http://100.127.255.254/",
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("expected CGNAT blocked: %s", u)
		}
	}
	// 100.128.x.x is outside CGNAT range
	if !IsSafeURL("https://100.128.0.1/") {
		t.Error("expected 100.128.0.1 to be allowed (outside CGNAT)")
	}
}

func TestIsSafeURL_PrivateRanges(t *testing.T) {
	blocked := []string{
		"http://172.16.0.1/",
		"http://172.20.0.1/",
		"http://172.31.255.254/",
		"http://169.254.1.1/",
		"http://[::1]/",
		"http://0.0.0.0/",
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("expected private range blocked: %s", u)
		}
	}
	// 172.32.x.x is outside private range
	if !IsSafeURL("https://172.32.0.1/") {
		t.Error("expected 172.32.0.1 to be allowed")
	}
}

func TestIsSafeURL_CloudMetadata(t *testing.T) {
	if IsSafeURL("http://metadata.google.internal/computeMetadata/v1/") {
		t.Error("expected Google metadata endpoint to be blocked")
	}
}

func TestIsSafeURL_DangerousSchemes(t *testing.T) {
	schemes := []string{
		"gopher://evil.com",
		"dict://evil.com",
		"ldap://evil.com",
		"telnet://evil.com",
		"tftp://evil.com",
		"data:text/html,<script>alert(1)</script>",
	}
	for _, u := range schemes {
		if IsSafeURL(u) {
			t.Errorf("expected dangerous scheme blocked: %s", u)
		}
	}
}

func TestValidateParams_Valid(t *testing.T) {
	valid, errJSON, err := ValidateParams("sessions.resolve", `{"key":"abc"}`)
	if !valid {
		t.Error("expected valid=true for well-formed params")
	}
	if errJSON != nil {
		t.Error("expected nil errorsJSON")
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
