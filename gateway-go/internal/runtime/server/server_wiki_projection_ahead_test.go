package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wikiProjectionAheadChildEnv = "DENEB_TEST_WIKI_PROJECTION_AHEAD_CHILD"

// A workspace whose generated MEMORY/USER view is AHEAD of the fact store is
// the one cutover problem that costs nothing: the never-go-backwards guard
// skips the write, the richer file stays readable, and the block clears itself
// once the journal passes that revision. It must therefore not spend the
// fail-closed restart budget — the end of which disables the whole wiki,
// trading recall and the wiki tool for a derived view that was already fine.
//
// Child process for the same reason as the cutover-failure suite: the wiki
// config caches its first environment read.
func TestServerStartsWithWikiEnabledWhenProjectionIsAhead(t *testing.T) {
	homeDir := t.TempDir()
	stateDir := filepath.Join(homeDir, "state")
	workspaceDir := filepath.Join(stateDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"MEMORY.md", "USER.md"} {
		body := fmt.Sprintf(
			"<!-- deneb:generated-fact-projection revision=126; source=.fact-mutations.jsonl; do-not-edit -->\n# %s\n",
			name,
		)
		if err := os.WriteFile(filepath.Join(workspaceDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestServerWikiProjectionAheadChild$")
	cmd.Env = append(
		envWithout(
			"HOME",
			"DENEB_CONFIG_PATH",
			"DENEB_PROFILE",
			"DENEB_STATE_DIR",
			"DENEB_WIKI_ENABLED",
			"DENEB_WIKI_DIR",
			"DENEB_WIKI_DIARY_DIR",
			wikiProjectionAheadChildEnv,
		),
		"HOME="+homeDir,
		"DENEB_STATE_DIR="+stateDir,
		"DENEB_WIKI_ENABLED=true",
		"DENEB_WIKI_DIR="+filepath.Join(stateDir, "wiki"),
		"DENEB_WIKI_DIARY_DIR="+filepath.Join(stateDir, "diary"),
		wikiProjectionAheadChildEnv+"=1",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, output)
	}

	// No failure was recorded, so a genuinely broken cutover later still gets
	// its full fail-closed budget instead of an already-spent one.
	data, err := os.ReadFile(filepath.Join(stateDir, factCutoverFileName))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read failure counter: %v", err)
	}
	var state factCutoverState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("parse failure counter %q: %v", data, err)
	}
	if state.ConsecutiveFailures != 0 {
		t.Fatalf("ahead projection burned %d cutover attempts, want 0 (last error %q)",
			state.ConsecutiveFailures, state.LastError)
	}
}

func TestServerWikiProjectionAheadChild(t *testing.T) {
	if os.Getenv(wikiProjectionAheadChildEnv) != "1" {
		t.Skip("child-process assertion")
	}

	server, err := New(":0", WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatalf("New failed on an ahead-of-store projection: %v", err)
	}
	defer server.Close(t.Context())

	if server.wikiStore == nil {
		t.Fatal("wiki was left unwired; an ahead projection must not disable the fact plane")
	}
	// The richer on-disk view is exactly what the guard was protecting.
	raw, err := os.ReadFile(filepath.Join(os.Getenv("DENEB_STATE_DIR"), "workspace", "MEMORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "revision=126") {
		t.Fatalf("MEMORY.md was overwritten: %q", raw)
	}
}
