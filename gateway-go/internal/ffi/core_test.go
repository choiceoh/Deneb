package ffi

import (
	"strings"
	"testing"
)

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

func TestValidateFrame_RequestMissingMethod(t *testing.T) {
	if err := ValidateFrame(`{"type":"req","id":"1"}`); err == nil {
		t.Error("expected error for request missing method")
	}
}

func TestValidateFrame_RequestMissingID(t *testing.T) {
	if err := ValidateFrame(`{"type":"req","method":"chat.send"}`); err == nil {
		t.Error("expected error for request missing id")
	}
}

func TestValidateFrame_ResponseMissingID(t *testing.T) {
	if err := ValidateFrame(`{"type":"res"}`); err == nil {
		t.Error("expected error for response missing id")
	}
}

func TestValidateFrame_EventMissingName(t *testing.T) {
	if err := ValidateFrame(`{"type":"event"}`); err == nil {
		t.Error("expected error for event missing event name")
	}
}

func TestValidateFrame_InvalidJSON(t *testing.T) {
	if err := ValidateFrame(`{not json}`); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDetectMIME_MoreFormats(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"GIF", []byte("GIF89a..."), "image/gif"},
		{"ZIP", []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00}, "application/zip"},
		{"JSON object", []byte(`{"key":"val"}`), "application/json"},
		{"JSON array", []byte(`[1,2,3]`), "application/json"},
		{"short 3 bytes", []byte{0x89, 0x50, 0x4E}, "application/octet-stream"},
		{"short 2 bytes", []byte{0xFF, 0xD8}, "application/octet-stream"},
		{"single byte", []byte{0x00}, "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectMIME(tt.data); got != tt.want {
				t.Errorf("DetectMIME() = %q, want %q", got, tt.want)
			}
		})
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

func TestIsSafeURL_EmptyHost(t *testing.T) {
	if IsSafeURL("http:///path") {
		t.Error("expected empty host to be blocked")
	}
}

func TestValidateSessionKey_LongKey(t *testing.T) {
	long := strings.Repeat("a", 513)
	if err := ValidateSessionKey(long); err == nil {
		t.Error("expected error for key exceeding 512 chars")
	}
	// 512 is OK
	ok := strings.Repeat("b", 512)
	if err := ValidateSessionKey(ok); err != nil {
		t.Errorf("expected 512-char key to be valid, got: %v", err)
	}
}

func TestValidateSessionKey_AllowedWhitespace(t *testing.T) {
	// Newline, tab, carriage return are allowed
	if err := ValidateSessionKey("key\twith\ntabs\r"); err != nil {
		t.Errorf("expected tabs/newlines to be allowed, got: %v", err)
	}
}

func TestSanitizeHTML_NoSpecialChars(t *testing.T) {
	input := "plain text with no special characters"
	got := SanitizeHTML(input)
	if got != input {
		t.Errorf("expected no-op for plain text, got %q", got)
	}
}

func TestSanitizeHTML_AllSpecialChars(t *testing.T) {
	got := SanitizeHTML(`<div class="x">&'test'</div>`)
	want := "&lt;div class=&quot;x&quot;&gt;&amp;&#x27;test&#x27;&lt;/div&gt;"
	if got != want {
		t.Errorf("SanitizeHTML = %q, want %q", got, want)
	}
}

func TestValidateParams_NoFFI(t *testing.T) {
	valid, errJSON, err := ValidateParams("any.method", `{"key":"value"}`)
	if valid {
		t.Error("expected valid=false in no_ffi build")
	}
	if errJSON != nil {
		t.Error("expected nil errorsJSON in no_ffi build")
	}
	if err == nil {
		t.Error("expected error in no_ffi build")
	}
}
