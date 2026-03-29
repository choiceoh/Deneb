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
		envClawdbot      string
		newDirExists     bool
		legacyDirsExist  []string
		expectedBasename string
	}{
		{
			label:            "DENEB_STATE_DIR wins over everything",
			envDeneb:         "/tmp/deneb-override",
			envClawdbot:      "/tmp/clawdbot-override",
			newDirExists:     true,
			legacyDirsExist:  []string{".clawdbot"},
			expectedBasename: "deneb-override",
		},
		{
			label:            "CLAWDBOT_STATE_DIR used when DENEB absent",
			envDeneb:         "",
			envClawdbot:      "/tmp/clawdbot-override",
			newDirExists:     false,
			legacyDirsExist:  nil,
			expectedBasename: "clawdbot-override",
		},
		{
			label:            "existing .deneb wins over legacy dirs",
			envDeneb:         "",
			envClawdbot:      "",
			newDirExists:     true,
			legacyDirsExist:  []string{".clawdbot"},
			expectedBasename: ".deneb",
		},
		{
			label:            "legacy dir used when .deneb absent",
			envDeneb:         "",
			envClawdbot:      "",
			newDirExists:     false,
			legacyDirsExist:  []string{".clawdbot"},
			expectedBasename: ".clawdbot",
		},
		{
			label:            "falls back to .deneb when nothing exists",
			envDeneb:         "",
			envClawdbot:      "",
			newDirExists:     false,
			legacyDirsExist:  nil,
			expectedBasename: ".deneb",
		},
	}

	policy := DefaultStateDirPolicy()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			home := t.TempDir()

			t.Setenv("DENEB_STATE_DIR", tc.envDeneb)
			t.Setenv("CLAWDBOT_STATE_DIR", tc.envClawdbot)

			if tc.newDirExists {
				if err := os.MkdirAll(filepath.Join(home, ".deneb"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			for _, legacy := range tc.legacyDirsExist {
				if err := os.MkdirAll(filepath.Join(home, legacy), 0755); err != nil {
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
		envClawdbot      string
		existingFiles    []string
		expectedBasename string
	}{
		{
			label:            "DENEB_CONFIG_PATH wins over everything",
			envDeneb:         "/tmp/custom.json",
			envClawdbot:      "/tmp/old.json",
			existingFiles:    []string{"deneb.json"},
			expectedBasename: "custom.json",
		},
		{
			label:            "CLAWDBOT_CONFIG_PATH used when DENEB absent",
			envDeneb:         "",
			envClawdbot:      "/tmp/old.json",
			existingFiles:    nil,
			expectedBasename: "old.json",
		},
		{
			label:            "canonical deneb.json preferred over legacy names",
			envDeneb:         "",
			envClawdbot:      "",
			existingFiles:    []string{"deneb.json", "clawdbot.json"},
			expectedBasename: "deneb.json",
		},
		{
			label:            "legacy clawdbot.json used when deneb.json absent",
			envDeneb:         "",
			envClawdbot:      "",
			existingFiles:    []string{"clawdbot.json"},
			expectedBasename: "clawdbot.json",
		},
		{
			label:            "defaults to deneb.json when none exist",
			envDeneb:         "",
			envClawdbot:      "",
			existingFiles:    nil,
			expectedBasename: "deneb.json",
		},
	}

	policy := DefaultConfigPathPolicy()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			stateDir := t.TempDir()

			t.Setenv("DENEB_CONFIG_PATH", tc.envDeneb)
			t.Setenv("CLAWDBOT_CONFIG_PATH", tc.envClawdbot)

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
		label       string
		envDeneb    string
		envClawdbot string
		configPort  *int
		expected    int
	}{
		{
			label:       "DENEB_GATEWAY_PORT wins over all",
			envDeneb:    "9001",
			envClawdbot: "9002",
			configPort:  intPtr(9003),
			expected:    9001,
		},
		{
			label:       "CLAWDBOT_GATEWAY_PORT used when DENEB absent",
			envDeneb:    "",
			envClawdbot: "9002",
			configPort:  intPtr(9003),
			expected:    9002,
		},
		{
			label:       "config port used when no env override",
			envDeneb:    "",
			envClawdbot: "",
			configPort:  intPtr(9003),
			expected:    9003,
		},
		{
			label:       "default used when nothing is set",
			envDeneb:    "",
			envClawdbot: "",
			configPort:  nil,
			expected:    DefaultGatewayPort,
		},
		{
			label:       "zero config port falls back to default",
			envDeneb:    "",
			envClawdbot: "",
			configPort:  intPtr(0),
			expected:    DefaultGatewayPort,
		},
		{
			label:       "invalid env port falls through to config",
			envDeneb:    "not-a-port",
			envClawdbot: "",
			configPort:  intPtr(9003),
			expected:    9003,
		},
	}

	policy := DefaultGatewayPortPolicy()

	for _, tc := range cases {
		tc := tc
		t.Run(tc.label, func(t *testing.T) {
			t.Setenv("DENEB_GATEWAY_PORT", tc.envDeneb)
			t.Setenv("CLAWDBOT_GATEWAY_PORT", tc.envClawdbot)

			got := policy.ResolveFrom(tc.configPort)
			if got != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, got)
			}
		})
	}
}

// ── Legacy compat helpers ──────────────────────────────────────────────────────

func TestFindLegacyStateDir(t *testing.T) {
	t.Run("returns empty when all absent", func(t *testing.T) {
		home := t.TempDir()
		if got := findLegacyStateDir(home); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("returns first match", func(t *testing.T) {
		home := t.TempDir()
		moldbot := filepath.Join(home, ".moldbot")
		if err := os.MkdirAll(moldbot, 0755); err != nil {
			t.Fatal(err)
		}
		if got := findLegacyStateDir(home); got != moldbot {
			t.Errorf("expected %q, got %q", moldbot, got)
		}
	})

	t.Run("prefers earlier entry", func(t *testing.T) {
		home := t.TempDir()
		clawdbot := filepath.Join(home, ".clawdbot")
		moldbot := filepath.Join(home, ".moldbot")
		if err := os.MkdirAll(clawdbot, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(moldbot, 0755); err != nil {
			t.Fatal(err)
		}
		if got := findLegacyStateDir(home); got != clawdbot {
			t.Errorf("expected %q, got %q", clawdbot, got)
		}
	})
}

func TestFindLegacyConfigFile(t *testing.T) {
	t.Run("returns empty when all absent", func(t *testing.T) {
		dir := t.TempDir()
		if got := findLegacyConfigFile(dir); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("returns first match", func(t *testing.T) {
		dir := t.TempDir()
		moldbot := filepath.Join(dir, "moldbot.json")
		if err := os.WriteFile(moldbot, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		if got := findLegacyConfigFile(dir); got != moldbot {
			t.Errorf("expected %q, got %q", moldbot, got)
		}
	})

	t.Run("prefers earlier entry", func(t *testing.T) {
		dir := t.TempDir()
		clawdbot := filepath.Join(dir, "clawdbot.json")
		moldbot := filepath.Join(dir, "moldbot.json")
		if err := os.WriteFile(clawdbot, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(moldbot, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		if got := findLegacyConfigFile(dir); got != clawdbot {
			t.Errorf("expected %q, got %q", clawdbot, got)
		}
	})
}
