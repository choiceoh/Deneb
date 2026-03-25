package media

import (
	"net"
	"testing"
)

func TestValidateURL(t *testing.T) {
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

func TestIsPrivateIP(t *testing.T) {
	privateIPs := []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1", "::1"}
	publicIPs := []string{"8.8.8.8", "1.1.1.1", "203.0.113.1"}

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

func TestDetectMIME(t *testing.T) {
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

