package media

import (
	"net"
	"testing"
)

func TestValidateURLRejectsPrivateAndUnsupportedSchemes(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid https", "https://example.com/image.png", false},
		{"valid http", "http://example.com/file.mp3", false},
		{"ftp blocked", "ftp://example.com/file", true},
		{"empty scheme", "://example.com", true},
		{"localhost blocked", "http://127.0.0.1/test", true},
		{"private 10.x blocked", "http://10.0.0.1/test", true},
		{"private 192.168 blocked", "http://192.168.1.1/test", true},
		{"private 172.16 blocked", "http://172.16.0.1/test", true},
		{"loopback ipv6 blocked", "http://[::1]/test", true},
		{"empty host", "http:///path", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestIsPrivateIPClassifiesPrivateRangesAndTreatsNilAsPrivate(t *testing.T) {
	privateIPs := []string{
		"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1", "::1",
		// CGNAT / tailnet range and other non-routable blocks.
		"100.64.0.1", "100.100.7.9", "192.0.0.8", "198.18.0.1", "224.0.0.251", "255.255.255.255",
		// Cloud metadata endpoints — Azure WireServer is a PUBLIC IP.
		"169.254.169.254", "168.63.129.16", "100.100.100.200",
		// IPv4-mapped IPv6 smuggling a private target.
		"::ffff:192.168.1.1", "::ffff:169.254.169.254",
		// IPv6 transition formats embedding a blocked IPv4:
		"2002:c0a8:101::",        // 6to4 → 192.168.1.1
		"2002:a9fe:a9fe::",       // 6to4 → 169.254.169.254 (metadata)
		"64:ff9b::a9fe:a9fe",     // NAT64 → 169.254.169.254
		"64:ff9b::808:808",       // NAT64 prefix blocked wholesale (even public embed)
		"2001:0:1234::3f57:fefe", // Teredo client XOR ff → 192.168.1.1
		"fe80::1", "fc00::1",
	}
	publicIPs := []string{"8.8.8.8", "1.1.1.1", "203.0.113.1", "2607:f8b0::1"}

	for _, ip := range privateIPs {
		if !isPrivateIP(net.ParseIP(ip)) {
			t.Errorf("expected %s to be private", ip)
		}
	}
	for _, ip := range publicIPs {
		if isPrivateIP(net.ParseIP(ip)) {
			t.Errorf("expected %s to be public", ip)
		}
	}
	if !isPrivateIP(nil) {
		t.Error("unparseable IP (nil) must be treated as dangerous")
	}
}

// TestEmbeddedIPv4sParsesTransitionFormatsAndSkipsPlainAddresses: each transition
// format decodes to the exact embedded IPv4, and plain addresses decode to nothing.
func TestEmbeddedIPv4sParsesTransitionFormatsAndSkipsPlainAddresses(t *testing.T) {
	cases := []struct {
		in   string
		want string // "" = no embedding
	}{
		{"2002:c0a8:101::", "192.168.1.1"},         // 6to4
		{"64:ff9b::c0a8:101", "192.168.1.1"},       // NAT64
		{"2001:0:1234::3f57:fefe", "192.168.1.1"},  // Teredo (XOR ff: 3f^ff=c0, 57^ff=a8, fe^ff=01)
		{"fe80::200:5efe:c0a8:101", "192.168.1.1"}, // ISATAP
		{"2607:f8b0::1", ""},                       // plain public IPv6
		{"8.8.8.8", ""},                            // IPv4 — no decode
	}
	for _, tc := range cases {
		got := embeddedIPv4s(net.ParseIP(tc.in))
		found := ""
		for _, ip := range got {
			if ip.String() == tc.want {
				found = tc.want
			}
		}
		if tc.want == "" && len(got) > 0 {
			t.Errorf("%s: expected no embedded IPv4, got %v", tc.in, got)
		}
		if tc.want != "" && found == "" {
			t.Errorf("%s: expected embedded %s, got %v", tc.in, tc.want, got)
		}
	}
}

func TestParseContentDispositionFileName(t *testing.T) {
	tests := []struct {
		header   string
		expected string
	}{
		{`attachment; filename="file.pdf"`, "file.pdf"},
		{`attachment; filename="path/to/file.pdf"`, "file.pdf"},
		{``, ""},
		{`inline`, ""},
	}
	for _, tt := range tests {
		got := parseContentDispositionFileName(tt.header)
		if got != tt.expected {
			t.Errorf("parseContentDispositionFileName(%q) = %q, want %q", tt.header, got, tt.expected)
		}
	}
}

func TestDetectMIMEIdentifiesPNGAndDefaultsOnEmptyInput(t *testing.T) {
	// PNG magic bytes.
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	if got := DetectMIME(png); got != "image/png" {
		t.Errorf("DetectMIME(png) = %q, want image/png", got)
	}

	// Empty.
	if got := DetectMIME(nil); got != "application/octet-stream" {
		t.Errorf("DetectMIME(nil) = %q, want application/octet-stream", got)
	}
}
