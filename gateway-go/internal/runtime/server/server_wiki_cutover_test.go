package server

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wikiCutoverFailureChildEnv = "DENEB_TEST_WIKI_CUTOVER_FAILURE_CHILD"

// TestServerNewFailsClosedOnWikiCutoverFailure uses child processes because the
// domain wiki config intentionally caches its first environment read. Each case
// therefore gets a fresh config singleton and proves the complete
// initMemorySubsystem -> buildSessionChatConfig -> registerSessionRPCMethods ->
// New error chain, rather than only testing an internal helper.
func TestServerNewFailsClosedOnWikiCutoverFailure(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		prepare func(t *testing.T, stateDir, workspaceDir string) string
	}{
		{
			name:    "store open",
			wantErr: "initialize enabled wiki store",
			prepare: func(t *testing.T, stateDir, _ string) string {
				t.Helper()
				blocker := filepath.Join(stateDir, "wiki-parent-file")
				if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(blocker, "wiki")
			},
		},
		{
			name:    "legacy import",
			wantErr: "import legacy facts",
			prepare: func(t *testing.T, stateDir, workspaceDir string) string {
				t.Helper()
				if err := os.Mkdir(filepath.Join(workspaceDir, "MEMORY.md"), 0o700); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(stateDir, "wiki")
			},
		},
		{
			name:    "fact projection",
			wantErr: "configure fact projections",
			prepare: func(t *testing.T, stateDir, workspaceDir string) string {
				t.Helper()
				memoryPath := filepath.Join(workspaceDir, "MEMORY.md")
				if err := os.WriteFile(memoryPath, []byte("# Memory\n\n- 기존 사실\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				// Projection prepares this exact sibling before its atomic rename.
				// A directory at the path deterministically fails the write without
				// relying on chmod behavior or the test runner's effective user.
				if err := os.Mkdir(memoryPath+".fact-prepare", 0o700); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(stateDir, "wiki")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := t.TempDir()
			stateDir := filepath.Join(homeDir, "state")
			workspaceDir := filepath.Join(homeDir, ".deneb", "workspace")
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
				t.Fatal(err)
			}
			wikiDir := tt.prepare(t, stateDir, workspaceDir)

			cmd := exec.Command(os.Args[0], "-test.run=^TestServerNewWikiCutoverFailureChild$")
			cmd.Env = append(envWithout(
				"HOME",
				"DENEB_CONFIG_PATH",
				"DENEB_PROFILE",
				"DENEB_STATE_DIR",
				"DENEB_WIKI_ENABLED",
				"DENEB_WIKI_DIR",
				"DENEB_WIKI_DIARY_DIR",
				wikiCutoverFailureChildEnv,
				"DENEB_TEST_WIKI_CUTOVER_WANT_ERROR",
			),
				"HOME="+homeDir,
				"DENEB_STATE_DIR="+stateDir,
				"DENEB_WIKI_ENABLED=true",
				"DENEB_WIKI_DIR="+wikiDir,
				"DENEB_WIKI_DIARY_DIR="+filepath.Join(stateDir, "diary"),
				wikiCutoverFailureChildEnv+"=1",
				"DENEB_TEST_WIKI_CUTOVER_WANT_ERROR="+tt.wantErr,
			)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child process failed: %v\n%s", err, output)
			}
		})
	}
}

func TestServerNewWikiCutoverFailureChild(t *testing.T) {
	if os.Getenv(wikiCutoverFailureChildEnv) != "1" {
		t.Skip("child-process assertion")
	}

	server, err := New(":0", WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err == nil {
		if server != nil {
			_ = server.Close(t.Context())
		}
		t.Fatal("New succeeded despite an enabled wiki cutover failure")
	}
	if server != nil {
		t.Fatalf("New returned a partially configured server on failure: %+v", server)
	}
	for _, fragment := range []string{
		"register session RPC methods",
		"build session chat config",
		"initialize memory subsystem",
		os.Getenv("DENEB_TEST_WIKI_CUTOVER_WANT_ERROR"),
	} {
		if fragment != "" && !strings.Contains(err.Error(), fragment) {
			t.Fatalf("New error %q does not contain stage %q", err, fragment)
		}
	}
}

func envWithout(keys ...string) []string {
	drop := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		drop[key] = struct{}{}
	}
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, ok := drop[key]; ok {
			continue
		}
		env = append(env, entry)
	}
	return env
}
