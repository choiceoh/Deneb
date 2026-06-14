package main

import (
	"strings"
	"testing"
)

func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name       string
		cfg        config
		wantSubstr string // a warning must contain this
		wantClean  bool   // or: expect no warnings
	}{
		{
			name:       "anthropic url missing /v1",
			cfg:        config{Models: []modelEntry{{Name: "glm", URL: "https://api.z.ai/api/anthropic", Protocol: "anthropic"}}},
			wantSubstr: "should end in /v1",
		},
		{
			name:      "anthropic url with /v1 is clean",
			cfg:       config{Models: []modelEntry{{Name: "glm", URL: "https://api.z.ai/api/anthropic/v1", Protocol: "anthropic"}}},
			wantClean: true,
		},
		{
			name:      "openai url needs no /v1 suffix check",
			cfg:       config{Models: []modelEntry{{Name: "dsv4", URL: "http://127.0.0.1:8000/v1"}}},
			wantClean: true,
		},
		{
			name:       "duplicate model name",
			cfg:        config{Models: []modelEntry{{Name: "x", URL: "http://a/v1"}, {Name: "x", URL: "http://b/v1"}}},
			wantSubstr: "duplicate model name x",
		},
		{
			name:       "unknown protocol",
			cfg:        config{Models: []modelEntry{{Name: "x", URL: "http://a/v1", Protocol: "grpc"}}},
			wantSubstr: "unknown protocol grpc",
		},
		{
			name:       "auto candidate not configured",
			cfg:        config{Auto: []string{"ghost"}, Models: []modelEntry{{Name: "x", URL: "http://a/v1"}}},
			wantSubstr: "auto candidate ghost is not a configured model",
		},
		{
			name:       "empty url",
			cfg:        config{Models: []modelEntry{{Name: "x", URL: ""}}},
			wantSubstr: "empty url",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warns := validateConfig(c.cfg)
			if c.wantClean {
				if len(warns) != 0 {
					t.Errorf("expected no warnings, got %v", warns)
				}
				return
			}
			found := false
			for _, w := range warns {
				if strings.Contains(w, c.wantSubstr) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a warning containing %q, got %v", c.wantSubstr, warns)
			}
		})
	}
}
