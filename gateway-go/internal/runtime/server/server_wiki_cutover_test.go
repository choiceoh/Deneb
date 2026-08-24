package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
)

const wikiCutoverFailureChildEnv = "DENEB_TEST_WIKI_CUTOVER_FAILURE_CHILD"

const wikiDisabledRevisionChildEnv = "DENEB_TEST_WIKI_DISABLED_REVISION_CHILD"

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
			cmd.Env = append(
				envWithout(
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

func TestServerNewWikiDisabledApprovesPromptSnapshotRevisionZero(t *testing.T) {
	homeDir := t.TempDir()
	stateDir := filepath.Join(homeDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const snapshot = `{"client:main:wiki-disabled":{"contextFiles":[{"Path":"MEMORY.md","Content":"frozen legacy memory"}],"factRevision":0}}`
	if err := os.WriteFile(filepath.Join(stateDir, "prompt_snapshots.json"), []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestServerNewWikiDisabledApprovesPromptSnapshotRevisionZeroChild$")
	cmd.Env = append(
		envWithout(
			"HOME",
			"DENEB_CONFIG_PATH",
			"DENEB_PROFILE",
			"DENEB_STATE_DIR",
			"DENEB_WIKI_ENABLED",
			wikiDisabledRevisionChildEnv,
		),
		"HOME="+homeDir,
		"DENEB_STATE_DIR="+stateDir,
		"DENEB_WIKI_ENABLED=false",
		wikiDisabledRevisionChildEnv+"=1",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, output)
	}
}

func TestServerNewWikiDisabledApprovesPromptSnapshotRevisionZeroChild(t *testing.T) {
	if os.Getenv(wikiDisabledRevisionChildEnv) != "1" {
		t.Skip("child-process assertion")
	}

	server, err := New(":0", WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatalf("New with wiki disabled: %v", err)
	}
	defer server.Close(t.Context())

	if restored := chat.LoadPromptSnapshots(func(string) bool { return true }); restored != 1 {
		t.Fatalf("restored prompt snapshots = %d, want 1 at approved revision zero", restored)
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

const wikiDegradeChildEnv = "DENEB_TEST_WIKI_DEGRADE_CHILD"

// TestServerStartsWithWikiDisabledAfterRepeatedCutoverFailures is the
// crash-loop regression. A deterministic cutover failure used to exit the
// process on every attempt forever (2026-08-23: 150 restarts, 28 minutes of
// downtime). The first attempts must still fail closed — a transient failure
// deserves its restart — but the streak has to terminate in a started gateway.
//
// Child processes share one state dir on purpose: the failure counter is
// persisted precisely because a process-local one would reset each restart and
// reproduce the loop.
func TestServerStartsWithWikiDisabledAfterRepeatedCutoverFailures(t *testing.T) {
	homeDir := t.TempDir()
	stateDir := filepath.Join(homeDir, "state")
	workspaceDir := filepath.Join(homeDir, ".deneb", "workspace")
	for _, dir := range []string{stateDir, workspaceDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// A directory where MEMORY.md belongs fails the legacy import identically
	// on every attempt, which is what "deterministic" means here.
	if err := os.Mkdir(filepath.Join(workspaceDir, "MEMORY.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	wikiDir := filepath.Join(stateDir, "wiki")

	startAttempt := func(t *testing.T, childTest, childEnv, wantErr string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^"+childTest+"$")
		cmd.Env = append(
			envWithout(
				"HOME",
				"DENEB_CONFIG_PATH",
				"DENEB_PROFILE",
				"DENEB_STATE_DIR",
				"DENEB_WIKI_ENABLED",
				"DENEB_WIKI_DIR",
				"DENEB_WIKI_DIARY_DIR",
				wikiCutoverFailureChildEnv,
				wikiDegradeChildEnv,
				"DENEB_TEST_WIKI_CUTOVER_WANT_ERROR",
			),
			"HOME="+homeDir,
			"DENEB_STATE_DIR="+stateDir,
			"DENEB_WIKI_ENABLED=true",
			"DENEB_WIKI_DIR="+wikiDir,
			"DENEB_WIKI_DIARY_DIR="+filepath.Join(stateDir, "diary"),
			childEnv+"=1",
			"DENEB_TEST_WIKI_CUTOVER_WANT_ERROR="+wantErr,
		)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child process failed: %v\n%s", err, output)
		}
	}

	for attempt := 1; attempt < maxFactCutoverAttempts; attempt++ {
		startAttempt(t, "TestServerNewWikiCutoverFailureChild", wikiCutoverFailureChildEnv, "import legacy facts")
	}
	startAttempt(t, "TestServerStartsWithWikiDisabledAfterRepeatedCutoverFailuresChild", wikiDegradeChildEnv, "")

	// Degrading is not succeeding, so the streak must persist: every later
	// restart degrades immediately instead of paying the fail-closed budget
	// again. It clears only when a cutover actually completes, which is what
	// happens once the operator fixes the cause.
	data, err := os.ReadFile(filepath.Join(stateDir, factCutoverFileName))
	if err != nil {
		t.Fatalf("read failure counter: %v", err)
	}
	var state factCutoverState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("parse failure counter %q: %v", data, err)
	}
	if state.ConsecutiveFailures != maxFactCutoverAttempts {
		t.Fatalf("consecutive failures = %d, want %d", state.ConsecutiveFailures, maxFactCutoverAttempts)
	}
	if !strings.Contains(state.LastError, "import legacy facts") {
		t.Fatalf("counter did not record the cause: %q", state.LastError)
	}
}

func TestServerStartsWithWikiDisabledAfterRepeatedCutoverFailuresChild(t *testing.T) {
	if os.Getenv(wikiDegradeChildEnv) != "1" {
		t.Skip("child-process assertion")
	}

	server, err := New(":0", WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatalf("New after %d consecutive cutover failures: %v", maxFactCutoverAttempts, err)
	}
	defer server.Close(t.Context())

	// Degraded means serving without the fact plane, not serving a half-built
	// one: a wired store here would be the hybrid state the fail-closed path
	// exists to prevent.
	if server.wikiStore != nil {
		t.Fatal("wiki store wired despite a deterministic cutover failure")
	}
}
