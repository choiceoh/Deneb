package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSessionThinkingDefaults_ParsesAgentDefaults verifies
// agents.defaults.thinking is read from deneb.json and surfaces as
// SessionDefaults the manager can install.
func TestResolveSessionThinkingDefaults_ParsesAgentDefaults(t *testing.T) {
	cases := []struct {
		name           string
		body           string
		wantLevel      string
		wantInterleavedNil bool
		wantInterleaved    bool
	}{
		{
			name: "level + interleaved present",
			body: `{
				"agents": {
					"defaults": {
						"thinking": {"level": "medium", "interleaved": true}
					}
				}
			}`,
			wantLevel:          "medium",
			wantInterleavedNil: false,
			wantInterleaved:    true,
		},
		{
			name: "level off normalises to empty string",
			body: `{
				"agents": {
					"defaults": {
						"thinking": {"level": "off"}
					}
				}
			}`,
			wantLevel:          "",
			wantInterleavedNil: true,
		},
		{
			name: "interleaved false is preserved (distinct from unset)",
			body: `{
				"agents": {
					"defaults": {
						"thinking": {"interleaved": false}
					}
				}
			}`,
			wantLevel:          "",
			wantInterleavedNil: false,
			wantInterleaved:    false,
		},
		{
			name:               "no thinking section",
			body:               `{"agents": {"defaults": {}}}`,
			wantLevel:          "",
			wantInterleavedNil: true,
		},
		{
			name:               "missing agents block",
			body:               `{"models": {}}`,
			wantLevel:          "",
			wantInterleavedNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "deneb.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			t.Setenv("DENEB_CONFIG_PATH", path)

			got := resolveSessionThinkingDefaults(slog.New(slog.NewTextHandler(io.Discard, nil)))
			if got.ThinkingLevel != tc.wantLevel {
				t.Errorf("level = %q, want %q", got.ThinkingLevel, tc.wantLevel)
			}
			if (got.InterleavedThinking == nil) != tc.wantInterleavedNil {
				t.Errorf("interleaved-nil = %v, want %v",
					got.InterleavedThinking == nil, tc.wantInterleavedNil)
			}
			if !tc.wantInterleavedNil {
				if *got.InterleavedThinking != tc.wantInterleaved {
					t.Errorf("interleaved = %v, want %v",
						*got.InterleavedThinking, tc.wantInterleaved)
				}
			}
		})
	}
}
