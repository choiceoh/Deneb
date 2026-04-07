package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ── StateDirPolicy precedence ─────────────────────────────────────────────────

func TestStateDirPolicyPrecedence(t *testing.T) {
	cases := []struct {
		label            string
		envDeneb         string
		newDirExists     bool
		expectedBasename string
	}{
		{
			label:            "DENEB_STATE_DIR wins",
			envDeneb:         "/tmp/deneb-override",
			newDirExists:     true,
			expectedBasename: "deneb-override",
		},
		{
			label:            "existing .deneb used when no env",
			envDeneb:         "",
			newDirExists:     true,
			expectedBasename: ".deneb",
		},
		{
			label:            "falls back to .deneb when nothing exists",
			envDeneb:         "",
			newDirExists:     false,
			expectedBasename: ".deneb",
		},
	}

	policy := DefaultStateDirPolicy()

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			home := t.TempDir()

			t.Setenv("DENEB_STATE_DIR", tc.envDeneb)

			if tc.newDirExists {
				if err := os.MkdirAll(filepath.Join(home, ".deneb"), 0755); err != nil {
					t.Fatal(err)
				}
			}

			got := policy.ResolveFrom(home)
			if filepath.Base(got) != tc.expectedBasename {
				t.Errorf("expected basename %q, got %q (full path: %q)",
					tc.expectedBasename, filepath.Base(got), got)
			}
		})
	}
}

// ── ConfigPathPolicy precedence ───────────────────────────────────────────────

func TestConfigPathPolicyPrecedence(t *testing.T) {
	cases := []struct {
		label            string
		envDeneb         string
		existingFiles    []string
		expectedBasename string
	}{
		{
			label:            "DENEB_CONFIG_PATH wins",
			envDeneb:         "/tmp/custom.json",
			existingFiles:    []string{"deneb.json"},
			expectedBasename: "custom.json",
		},
		{
			label:            "deneb.json used when present",
			envDeneb:         "",
			existingFiles:    []string{"deneb.json"},
			expectedBasename: "deneb.json",
		},
		{
			label:            "defaults to deneb.json when none exist",
			envDeneb:         "",
			existingFiles:    nil,
			expectedBasename: "deneb.json",
		},
	}

	policy := DefaultConfigPathPolicy()

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			stateDir := t.TempDir()

			t.Setenv("DENEB_CONFIG_PATH", tc.envDeneb)

			for _, name := range tc.existingFiles {
				if err := os.WriteFile(filepath.Join(stateDir, name), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			}

			got := policy.ResolveFrom(stateDir)
			if filepath.Base(got) != tc.expectedBasename {
				t.Errorf("expected basename %q, got %q (full path: %q)",
					tc.expectedBasename, filepath.Base(got), got)
			}
		})
	}
}

// ── GatewayPortPolicy precedence ──────────────────────────────────────────────

func TestGatewayPortPolicyPrecedence(t *testing.T) {
	intPtr := func(v int) *int { return &v }

	cases := []struct {
		label      string
		envDeneb   string
		configPort *int
		expected   int
	}{
		{
			label:      "DENEB_GATEWAY_PORT wins over all",
			envDeneb:   "9001",
			configPort: intPtr(9003),
			expected:   9001,
		},
		{
			label:      "config port used when no env override",
			envDeneb:   "",
			configPort: intPtr(9003),
			expected:   9003,
		},
		{
			label:      "default used when nothing is set",
			envDeneb:   "",
			configPort: nil,
			expected:   DefaultGatewayPort,
		},
		{
			label:      "zero config port falls back to default",
			envDeneb:   "",
			configPort: intPtr(0),
			expected:   DefaultGatewayPort,
		},
		{
			label:      "invalid env port falls through to config",
			envDeneb:   "not-a-port",
			configPort: intPtr(9003),
			expected:   9003,
		},
	}

	policy := DefaultGatewayPortPolicy()

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			t.Setenv("DENEB_GATEWAY_PORT", tc.envDeneb)

			got := policy.ResolveFrom(tc.configPort)
			if got != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, got)
			}
		})
	}
}
