package server

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/goals"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/evenapi"
	runtimeheartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
)

const (
	sessionStateChildEnv = "DENEB_TEST_SESSION_STATE_DIR_CHILD"
	// sessionStateProbeKey is the conversation the child titles, pins, focuses
	// and gives a goal; where its rows land is which dir each store resolved.
	sessionStateProbeKey = "client:main:statedir-probe"
)

// sessionStateFiles are the server's stores that used to be built from $HOME
// (see stateFilePath and registerWorkflowSideEffects). The child writes every
// one of them.
var sessionStateFiles = []string{
	"session-labels.json",
	"session-pins.json",
	"session-models.json",
	"session-list-pins.json",
	"session-focus.json",
	"session-repos.json",
	"goals.json",
	"even-imu-samples.jsonl",
}

// A child process boots a real gateway in the dev-gateway shape — HOME and
// DENEB_STATE_DIR at DIFFERENT temp dirs — and writes through the server's own
// paths: the focus and repo sidecars directly, labels/pins/models/list pins
// through the persistence sweep's shutdown flush, the goal store the
// side-effect wiring installs, and the Even IMU log. Each must land under the
// state dir, and $HOME/.deneb — present, as the production dir is on the dev
// host — must stay empty. Child, not in-process: server construction caches
// env-derived paths (the wiki config) that would outlive this test's temp dirs.
func TestGatewayWritesSessionStateUnderTheStateDir(t *testing.T) {
	homeDir := t.TempDir()
	stateDir := t.TempDir()
	homeState := filepath.Join(homeDir, ".deneb")
	if err := os.MkdirAll(homeState, 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestGatewaySessionStateDirChild$")
	cmd.Env = append(
		envWithout("HOME", "DENEB_STATE_DIR", "DENEB_CONFIG_PATH", "DENEB_PROFILE", sessionStateChildEnv),
		"HOME="+homeDir,
		"DENEB_STATE_DIR="+stateDir,
		sessionStateChildEnv+"=1",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child gateway failed: %v\n%s", err, output)
	}

	for _, name := range sessionStateFiles {
		data, err := os.ReadFile(filepath.Join(stateDir, name))
		if err != nil {
			t.Errorf("%s did not land under the state dir: %v", name, err)
			continue
		}
		// session-repos.json holds no binding for the probe (the child only
		// forgets a conversation) and the IMU log is keyed by label.
		if name == "session-repos.json" || name == "even-imu-samples.jsonl" {
			continue
		}
		if !strings.Contains(string(data), sessionStateProbeKey) {
			t.Errorf("%s = %q, want the child's %s row", name, data, sessionStateProbeKey)
		}
	}

	entries, err := os.ReadDir(homeState)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("$HOME/.deneb = %v, want it untouched — a store still resolves from $HOME", names)
	}
}

func TestGatewaySessionStateDirChild(t *testing.T) {
	if os.Getenv(sessionStateChildEnv) != "1" {
		t.Skip("child-process assertion")
	}

	srv, err := New(":0", WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// StartAndListen starts this sweep; its shutdown flush writes the label,
	// pin, model and list-pin sidecars.
	srv.startSessionLabelPersistence()

	label, model, yes := "상태 디렉터리 확인", "probe/model", true
	srv.sessions.Create(sessionStateProbeKey, session.KindDirect)
	srv.sessions.Patch(sessionStateProbeKey, session.PatchFields{
		Label: &label, LabelPinned: &yes, Pinned: &yes, Model: &model,
	})
	if err := srv.setSessionFocus(sessionStateProbeKey); err != nil {
		t.Fatalf("setSessionFocus: %v", err)
	}
	// Forgetting a conversation rewrites the repo-binding sidecar.
	srv.forgetSessionExtras("client:main:statedir-gone")

	store := goals.Default()
	if store == nil {
		t.Fatal("goal store left unwired")
	}
	store.Set(sessionStateProbeKey, "상태 디렉터리 확인", 1)

	if err := srv.evenRecordImu()(evenapi.ImuRecording{At: "probe", Label: "statedir-probe"}); err != nil {
		t.Fatalf("evenRecordImu: %v", err)
	}

	if err := srv.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The final flush runs on the sweep goroutine after the lifecycle context
	// is cancelled; stay up until it lands so the parent sees its effect. A
	// sweep resolved anywhere else never writes these and times out here.
	stateDir := os.Getenv("DENEB_STATE_DIR")
	deadline := time.Now().Add(10 * time.Second)
	for _, name := range []string{"session-labels.json", "session-pins.json", "session-models.json", "session-list-pins.json"} {
		for {
			if _, err := os.Stat(filepath.Join(stateDir, name)); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("shutdown flush never wrote %s under the state dir", name)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// Production must be unchanged byte-for-byte: the unit pins
// DENEB_STATE_DIR=$HOME/.deneb, and an unset one defaults there too — nothing
// to migrate.
func TestSessionStateProductionPathsUnchanged(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sidecars := map[string]func() (string, error){
		"session-labels.json":    sessionLabelStorePath,
		"session-pins.json":      sessionPinsStorePath,
		"session-models.json":    sessionModelsStorePath,
		"session-list-pins.json": sessionListPinsStorePath,
		"session-focus.json":     sessionFocusStorePath,
		"session-repos.json":     sessionReposStorePath,
	}
	for _, stateDir := range []string{"", filepath.Join(home, ".deneb")} {
		t.Setenv("DENEB_STATE_DIR", stateDir)
		for name, resolve := range sidecars {
			got, err := resolve()
			if want := filepath.Join(home, ".deneb", name); err != nil || got != want {
				t.Errorf("DENEB_STATE_DIR=%q: %s resolved to %q (err %v), want %q", stateDir, name, got, err, want)
			}
		}
		if got, want := runtimeheartbeat.FixturePath(config.ResolveStateDir()),
			filepath.Join(home, ".deneb", "data", "heartbeat_fixtures.jsonl"); got != want {
			t.Errorf("DENEB_STATE_DIR=%q: heartbeat fixtures = %q, want %q", stateDir, got, want)
		}
	}
}
