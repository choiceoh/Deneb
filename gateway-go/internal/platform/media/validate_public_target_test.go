package media

import (
	"context"
	"strings"
	"testing"
)

// All cases use IP literals or invalid schemes so no DNS resolution happens —
// the resolver path is covered by the private-literal short-circuit contract.
func TestValidatePublicTargetLiteralContract(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string // "" = accept
	}{
		{"public literal", "https://8.8.8.8/path", ""},
		{"loopback blocked", "http://127.0.0.1:18789/health", "SSRF"},
		{"rfc1918 blocked", "http://192.168.0.10/", "SSRF"},
		{"cgnat tailnet blocked", "http://100.105.145.6:18900/", "SSRF"},
		{"metadata ip blocked", "http://169.254.169.254/latest/", "SSRF"},
		{"ipv6 loopback blocked", "http://[::1]/", "SSRF"},
		{"bad scheme", "ftp://example.com/x", "unsupported scheme"},
		{"empty host", "http:///x", "empty hostname"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePublicTarget(context.Background(), tt.url)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePublicTarget(%q) = %v, want nil", tt.url, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePublicTarget(%q) = %v, want containing %q", tt.url, err, tt.wantErr)
			}
		})
	}
}
