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
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

const (
	agentLogStateDirChildEnv = "DENEB_TEST_AGENTLOG_STATE_DIR_CHILD"
	// agentLogProbeSession is the key the child gateway logs under; where its
	// file lands is which directory the server's writer resolved.
	agentLogProbeSession = "system:statedir-probe"
	// agentLogStaleName is planted past retention in BOTH candidate dirs.
	agentLogStaleName = "client:main.jsonl"
)

// The agent-log writer used to be built from $HOME, so every dev, live-test and
// puppet gateway appended its runs to the operator's real log. Measured
// 2026-09-19: a dev run against fake engines left "fake-fallback"/"fake-tiny"
// model rows in production boot.jsonl and system:helper.jsonl — rows the model
// tuner's AggregateByModel folds into its scorecard — and a dev Aurora Dream
// failure as a delivered proactive.relay. Each dev start also ran the retention
// sweep, which deletes files, over the production directory.
//
// A child process boots a real gateway with HOME and DENEB_STATE_DIR pointing
// at DIFFERENT temp dirs (the dev-gateway shape) and logs through the server's
// own writer — the one instance chat, the proactive relay and the observe RPCs
// share. Child, not in-process: server construction caches env-derived paths
// (the wiki config) that would outlive this test's temp dirs.
func TestGatewayWritesAgentLogsUnderTheStateDir(t *testing.T) {
	homeDir := t.TempDir()
	stateDir := t.TempDir()
	homeLogs := filepath.Join(homeDir, ".deneb", "agent-logs")
	stateLogs := filepath.Join(stateDir, "agent-logs")

	stale := time.Now().Add(-200 * 24 * time.Hour) // retention keeps 180d
	plantStaleAgentLog(t, homeLogs, stale)
	plantStaleAgentLog(t, stateLogs, stale)

	cmd := exec.Command(os.Args[0], "-test.run=^TestGatewayAgentLogStateDirChild$")
	cmd.Env = append(
		envWithout("HOME", "DENEB_STATE_DIR", "DENEB_CONFIG_PATH", "DENEB_PROFILE", agentLogStateDirChildEnv),
		"HOME="+homeDir,
		"DENEB_STATE_DIR="+stateDir,
		agentLogStateDirChildEnv+"=1",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child gateway failed: %v\n%s", err, output)
	}

	probe, err := os.ReadFile(filepath.Join(stateLogs, agentLogProbeSession+".jsonl"))
	if err != nil {
		t.Fatalf("probe event did not land under the state dir: %v", err)
	}
	if !strings.Contains(string(probe), `"statedir-probe"`) {
		t.Fatalf("probe file holds %q, want the child's event", probe)
	}
	if _, err := os.Stat(filepath.Join(stateLogs, agentLogStaleName)); !os.IsNotExist(err) {
		t.Fatalf("retention sweep skipped the state dir (stat err = %v)", err)
	}

	// $HOME's log dir is exactly as planted: no probe, and the stale file the
	// sweep would have deleted is intact.
	entries, err := os.ReadDir(homeLogs)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != agentLogStaleName {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("$HOME/.deneb/agent-logs = %v, want only the untouched %s", names, agentLogStaleName)
	}
}

func TestGatewayAgentLogStateDirChild(t *testing.T) {
	if os.Getenv(agentLogStateDirChildEnv) != "1" {
		t.Skip("child-process assertion")
	}

	srv, err := New(":0", WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer srv.Close(t.Context())

	if srv.agentLogWriter == nil {
		t.Fatal("agent-log writer left unwired")
	}
	srv.agentLogWriter.LogEvent(agentLogProbeSession, agentlog.TypeBackgroundJob,
		json.RawMessage(`{"job":"statedir-probe"}`))

	// Retention runs off the startup path; stay up until it has swept, so the
	// parent's assertions see its finished effect. A writer resolved anywhere
	// else never removes this file and times out here.
	stateStale := filepath.Join(os.Getenv("DENEB_STATE_DIR"), "agent-logs", agentLogStaleName)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(stateStale); os.IsNotExist(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("retention sweep never removed the stale state-dir log")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Production must be unchanged byte-for-byte: the unit pins
// DENEB_STATE_DIR=$HOME/.deneb, and an unset one defaults there too — nothing
// to migrate.
func TestAgentLogDirProductionPathUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".deneb", "agent-logs")
	for _, stateDir := range []string{"", filepath.Join(home, ".deneb")} {
		t.Setenv("DENEB_STATE_DIR", stateDir)
		if got := agentLogDir(); got != want {
			t.Fatalf("DENEB_STATE_DIR=%q: agent-log dir = %q, want %q", stateDir, got, want)
		}
	}
}

func plantStaleAgentLog(t *testing.T, dir string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, agentLogStaleName)
	line := `{"ts":1,"type":"run.start","runId":"run_0000","session":"client:main","data":{}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}
