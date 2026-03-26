package auth

import (
	"testing"
)

func TestNormalizeInputHostnameAllowlist(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect []string
	}{
		{"nil input", nil, nil},
		{"empty input", []string{}, nil},
		{"whitespace only", []string{"  ", "\t", ""}, nil},
		{"trims entries", []string{"  example.com  ", "test.io"}, []string{"example.com", "test.io"}},
		{"filters empty", []string{"example.com", "", "test.io"}, []string{"example.com", "test.io"}},
		{"single valid", []string{"example.com"}, []string{"example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeInputHostnameAllowlist(tt.input)
			if tt.expect == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.expect) {
				t.Errorf("length mismatch: got %d, want %d", len(got), len(tt.expect))
				return
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.expect[i])
				}
			}
		})
	}
}
