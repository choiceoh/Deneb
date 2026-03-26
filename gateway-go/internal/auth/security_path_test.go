package auth

import "testing"

func TestCanonicalizePathForSecurity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantPath string
	}{
		{"root", "/", "/"},
		{"simple path", "/api/v1", "/api/v1"},
		{"trailing slash", "/api/v1/", "/api/v1"},
		{"double slash", "//api//v1", "/api/v1"},
		{"dot segments", "/api/../v1", "/v1"},
		{"uppercase", "/API/V1", "/api/v1"},
		{"encoded slash", "/api%2Fv1", "/api/v1"},
		{"double encoded slash", "/api%252Fv1", "/api/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CanonicalizePathForSecurity(tt.input)
			if result.CanonicalPath != tt.wantPath {
				t.Errorf("CanonicalPath = %q, want %q", result.CanonicalPath, tt.wantPath)
			}
		})
	}
}

func TestIsPathProtectedByPrefixes(t *testing.T) {
	prefixes := []string{"/api/channels"}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"exact match", "/api/channels", true},
		{"subpath", "/api/channels/telegram", true},
		{"no match", "/api/v1/rpc", false},
		{"case bypass attempt", "/API/CHANNELS", true},
		{"encoded bypass", "/api%2Fchannels", true},
		{"double encoded bypass", "%2Fapi%2Fchannels", true},
		{"dot segment bypass", "/api/foo/../channels", true},
		{"unrelated path", "/health", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPathProtectedByPrefixes(tt.path, prefixes)
			if got != tt.want {
				t.Errorf("IsPathProtectedByPrefixes(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsProtectedPluginRoutePath(t *testing.T) {
	if !IsProtectedPluginRoutePath("/api/channels/telegram") {
		t.Error("expected /api/channels/telegram to be protected")
	}
	if IsProtectedPluginRoutePath("/health") {
		t.Error("expected /health to not be protected")
	}
}
