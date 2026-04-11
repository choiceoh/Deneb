package coresecurity

import (
	"strings"
	"testing"
)

// --- ValidateSessionKey (Rust: test_is_valid_session_key) ---

func TestValidateSessionKey(t *testing.T) {
	if err := ValidateSessionKey("my-session-123"); err != nil {
		t.Errorf("valid key rejected: %v", err)
	}
	if err := ValidateSessionKey("a"); err != nil {
		t.Errorf("min length key rejected: %v", err)
	}
	if err := ValidateSessionKey(""); err == nil {
		t.Error("empty key accepted")
	}
	if err := ValidateSessionKey(strings.Repeat("x", 513)); err == nil {
		t.Error("513-char key accepted")
	}
	if err := ValidateSessionKey(strings.Repeat("x", 512)); err != nil {
		t.Errorf("512-char key rejected: %v", err)
	}
	if err := ValidateSessionKey("has\x00null"); err == nil {
		t.Error("key with null accepted")
	}
}

// --- ValidateSessionKey multibyte (Rust: test_is_valid_session_key_multibyte) ---

func TestValidateSessionKey_Multibyte(t *testing.T) {
	key512 := strings.Repeat("a", 512)
	if err := ValidateSessionKey(key512); err != nil {
		t.Errorf("512 ASCII chars rejected: %v", err)
	}
	key513 := strings.Repeat("a", 513)
	if err := ValidateSessionKey(key513); err == nil {
		t.Error("513 ASCII chars accepted")
	}
	// 256 two-byte runes = 256 chars, 512 bytes — under 512 char limit.
	multibyte := strings.Repeat("\u00e9", 256)
	if len([]rune(multibyte)) != 256 {
		t.Fatal("test setup: expected 256 runes")
	}
	if len(multibyte) != 512 {
		t.Fatal("test setup: expected 512 bytes")
	}
	if err := ValidateSessionKey(multibyte); err != nil {
		t.Errorf("256-rune multibyte key rejected: %v", err)
	}
}

// --- SanitizeHTML (Rust: test_sanitize_html) ---

func TestSanitizeHTML(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{"a & b", "a &amp; b"},
		{`"quoted"`, "&quot;quoted&quot;"},
		{"it's", "it&#x27;s"},
		{`<div class="x">a & b</div>`, `&lt;div class=&quot;x&quot;&gt;a &amp; b&lt;/div&gt;`},
	}
	for _, tc := range cases {
		if got := SanitizeHTML(tc.input); got != tc.want {
			t.Errorf("SanitizeHTML(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- IsSafeURL (Rust: test_is_safe_url) ---

func TestIsSafeURL(t *testing.T) {
	safe := []string{
		"https://example.com/api",
		"http://cdn.example.com/image.png",
		"http://172.32.0.1/",       // 172.32 is public
		"http://user@example.com/", // public host with userinfo
	}
	for _, u := range safe {
		if !IsSafeURL(u) {
			t.Errorf("IsSafeURL(%q) = false, want true", u)
		}
	}

	blocked := []string{
		"http://localhost/admin",
		"http://127.0.0.1:8080/",
		"http://0.0.0.0/",
		"http://10.0.0.1/secret",
		"http://192.168.1.1/",
		"http://172.16.0.1/",
		"http://172.31.255.255/",
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/",
		"ftp://example.com/file",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"",
		"http://",
		"http://evil@localhost/",
		"http://user:pass@127.0.0.1/",
		"http://anything@10.0.0.1/secret",
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("IsSafeURL(%q) = true, want false", u)
		}
	}
}

// --- IPv6 (Rust: test_is_safe_url_ipv6) ---

func TestIsSafeURL_IPv6(t *testing.T) {
	blocked := []string{
		"http://[::1]/",
		"http://[::1]:8080/path",
		"http://[::ffff:127.0.0.1]/",
		"http://[::ffff:10.0.0.1]/",
		"http://[::ffff:192.168.1.1]/",
		"http://[fd12:3456::1]/",
		"http://[fc00::1]/",
		"http://[fe80::1]/",
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("IsSafeURL(%q) = true, want false", u)
		}
	}
	if !IsSafeURL("http://[2001:db8::1]/") {
		t.Error("public IPv6 should be safe")
	}
}

// --- Metadata IPv6 (Rust: test_is_safe_url_metadata_ipv6) ---

func TestIsSafeURL_MetadataIPv6(t *testing.T) {
	if IsSafeURL("http://[::ffff:169.254.169.254]/") {
		t.Error("cloud metadata via IPv4-mapped IPv6 should be blocked")
	}
}

// --- Numeric bypass (Rust: test_is_safe_url_numeric_bypass) ---

func TestIsSafeURL_NumericBypass(t *testing.T) {
	blocked := []string{
		"http://0177.0.0.1/",       // octal 127.0.0.1
		"http://0177.0.0.01/admin", // octal 127.0.0.1
		"http://0x7f000001/",       // hex 127.0.0.1
		"http://0X7F000001/",       // hex uppercase
		"http://2130706433/",       // decimal 127.0.0.1
		"http://012.0.0.01/",       // octal 10.0.0.1
		"http://0xC0A80101/",       // hex 192.168.1.1
		"http://2852039166/",       // decimal 169.254.169.254
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("IsSafeURL(%q) = true, want false (numeric bypass)", u)
		}
	}
	// Public IP in decimal should pass (8.8.8.8 = 134744072).
	if !IsSafeURL("http://134744072/") {
		t.Error("public decimal IP 134744072 (8.8.8.8) should be safe")
	}
	// Normal dotted decimal still caught by existing checks.
	if IsSafeURL("http://127.0.0.1/") {
		t.Error("127.0.0.1 should be blocked")
	}
	if !IsSafeURL("http://8.8.8.8/") {
		t.Error("8.8.8.8 should be safe")
	}
}

// --- IPv6 zone ID (Rust: test_is_safe_url_ipv6_zone_id) ---

func TestIsSafeURL_IPv6ZoneID(t *testing.T) {
	blocked := []string{
		"http://[fe80::1%25eth0]/",
		"http://[::1%25lo]/",
		"http://[fe80::1%eth0]/",
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("IsSafeURL(%q) = true, want false (zone ID)", u)
		}
	}
}

// --- File URL (Rust: test_file_url_blocked) ---

func TestIsSafeURL_FileURL(t *testing.T) {
	blocked := []string{
		"file:///etc/passwd",
		"FILE:///etc/passwd",
		"File:///etc/passwd",
		"file://localhost/etc/passwd",
		"file:///C:/Windows/System32",
		"file:\\\\C:\\Windows\\System32",
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("IsSafeURL(%q) = true, want false (file URL)", u)
		}
	}
}

// --- UNC path (Rust: test_unc_path_blocked) ---

func TestIsSafeURL_UNCPath(t *testing.T) {
	blocked := []string{
		"\\\\server\\share",
		"\\\\?\\UNC\\server\\share",
		"//server/share",
		"//169.254.169.254/latest/meta-data",
	}
	for _, u := range blocked {
		if IsSafeURL(u) {
			t.Errorf("IsSafeURL(%q) = true, want false (UNC path)", u)
		}
	}
}

// --- Internal helpers ---

func TestIsPrivateIPv4U32(t *testing.T) {
	cases := []struct {
		ip   uint32
		want bool
	}{
		{0x7f000001, true},  // 127.0.0.1
		{0x00000000, true},  // 0.0.0.0
		{0x0a000001, true},  // 10.0.0.1
		{0xac100001, true},  // 172.16.0.1
		{0xc0a80101, true},  // 192.168.1.1
		{0xa9fe0001, true},  // 169.254.0.1
		{0x64400001, true},  // 100.64.0.1 (CGNAT)
		{0x08080808, false}, // 8.8.8.8
	}
	for _, tc := range cases {
		if got := isPrivateIPv4U32(tc.ip); got != tc.want {
			t.Errorf("isPrivateIPv4U32(0x%08x) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

func TestParseOctetMixedRadix(t *testing.T) {
	cases := []struct {
		s    string
		want byte
		ok   bool
	}{
		{"0", 0, true},
		{"127", 127, true},
		{"0177", 127, true}, // octal
		{"0x7f", 127, true}, // hex
		{"0X7F", 127, true}, // hex uppercase
		{"0xff", 255, true}, // hex max
		{"0377", 255, true}, // octal max
		{"256", 0, false},   // overflow
		{"0400", 0, false},  // octal overflow
		{"0xfff", 0, false}, // hex overflow
		{"", 0, false},      // empty
	}
	for _, tc := range cases {
		got, ok := parseOctetMixedRadix(tc.s)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseOctetMixedRadix(%q) = (%d, %v), want (%d, %v)", tc.s, got, ok, tc.want, tc.ok)
		}
	}
}
