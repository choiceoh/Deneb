package server

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// TestShouldReleaseSpillover mirrors the checkpoint routing table — the two
// decision functions must remain in sync, so we assert the same scenarios.
func TestShouldReleaseSpillover(t *testing.T) {
	cases := []struct {
		name  string
		event session.Event
		want  bool
	}{
		{"delete always releases", session.Event{Kind: session.EventDeleted, Key: "k"}, true},
		{"status → done releases", session.Event{Kind: session.EventStatusChanged, NewStatus: session.StatusDone}, true},
		{"status → failed releases", session.Event{Kind: session.EventStatusChanged, NewStatus: session.StatusFailed}, true},
		{"status → killed releases", session.Event{Kind: session.EventStatusChanged, NewStatus: session.StatusKilled}, true},
		{"status → timeout releases", session.Event{Kind: session.EventStatusChanged, NewStatus: session.StatusTimeout}, true},
		{"reset (empty status) releases", session.Event{Kind: session.EventStatusChanged, NewStatus: ""}, true},
		{"status → running does NOT release", session.Event{Kind: session.EventStatusChanged, NewStatus: session.StatusRunning}, false},
		{"create does NOT release", session.Event{Kind: session.EventCreated}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldReleaseSpillover(tc.event); got != tc.want {
				t.Errorf("shouldReleaseSpillover(%+v) = %v, want %v", tc.event, got, tc.want)
			}
		})
	}
}

// TestSpilloverLifecycle_RemovesOnTerminal drives a session from start → end
// and asserts that the spill file belonging to that session is reclaimed. A
// spill file from a different session must survive — this is the isolation
// guarantee that makes the lifecycle hook safe to enable by default.
func TestSpilloverLifecycle_RemovesOnTerminal(t *testing.T) {
	dir := t.TempDir()
	store := agent.NewSpilloverStore(dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &Server{
		ServerTransport: &ServerTransport{},
		ServerRPC:       &ServerRPC{},
		ServerRuntime:   &ServerRuntime{},
		SessionManager:  &SessionManager{sessions: session.NewManager()},
		ChatManager:     &ChatManager{},
		HookManager:     &HookManager{},
		logger:          logger,
	}
	s.initSpilloverLifecycle(store)
	t.Cleanup(func() {
		if s.spilloverLifecycleUnsub != nil {
			s.spilloverLifecycleUnsub()
		}
	})

	const doomedKey = "sess-doomed"
	const keepKey = "sess-keep"

	// Seed one spill per session.
	content := strings.Repeat("x", agent.MaxResultChars+1)
	doomedID, err := store.Store(doomedKey, "read", content)
	if err != nil {
		t.Fatalf("seed doomed spill: %v", err)
	}
	keepID, err := store.Store(keepKey, "grep", content)
	if err != nil {
		t.Fatalf("seed keep spill: %v", err)
	}

	// Drive the doomed session through start → end.
	s.sessions.ApplyLifecycleEvent(doomedKey, session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1})
	s.sessions.ApplyLifecycleEvent(doomedKey, session.LifecycleEvent{Phase: session.PhaseEnd, Ts: 2})

	if !waitForSpillGone(store, doomedID, doomedKey, 2*time.Second) {
		t.Fatalf("doomed spill %s still exists after terminal transition", doomedID)
	}

	// keep session must survive. Also verify no files with the doomed prefix
	// linger on disk.
	if _, err := store.Load(keepID, keepKey); err != nil {
		t.Errorf("keep spill should survive terminal of sibling session: %v", err)
	}
	if anyFileHasPrefix(t, dir, "sess_doomed_") {
		t.Errorf("disk still has files with doomed session prefix")
	}
}

// TestSpilloverLifecycle_RemovesOnReset verifies the /reset path (empty status).
func TestSpilloverLifecycle_RemovesOnReset(t *testing.T) {
	dir := t.TempDir()
	store := agent.NewSpilloverStore(dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &Server{
		ServerTransport: &ServerTransport{},
		ServerRPC:       &ServerRPC{},
		ServerRuntime:   &ServerRuntime{},
		SessionManager:  &SessionManager{sessions: session.NewManager()},
		ChatManager:     &ChatManager{},
		HookManager:     &HookManager{},
		logger:          logger,
	}
	s.initSpilloverLifecycle(store)
	t.Cleanup(func() {
		if s.spilloverLifecycleUnsub != nil {
			s.spilloverLifecycleUnsub()
		}
	})

	const key = "sess-reset"
	s.sessions.ApplyLifecycleEvent(key, session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1})
	content := strings.Repeat("r", agent.MaxResultChars+1)
	spillID, err := store.Store(key, "exec", content)
	if err != nil {
		t.Fatalf("seed spill: %v", err)
	}

	if reset := s.sessions.ResetSession(key); reset == nil {
		t.Fatal("ResetSession returned nil — precondition not met")
	}
	if !waitForSpillGone(store, spillID, key, 2*time.Second) {
		t.Fatalf("spill should be removed after /reset")
	}
}

// TestSpilloverLifecycle_IgnoresRunningTransition — running → do not wipe.
func TestSpilloverLifecycle_IgnoresRunningTransition(t *testing.T) {
	dir := t.TempDir()
	store := agent.NewSpilloverStore(dir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &Server{
		ServerTransport: &ServerTransport{},
		ServerRPC:       &ServerRPC{},
		ServerRuntime:   &ServerRuntime{},
		SessionManager:  &SessionManager{sessions: session.NewManager()},
		ChatManager:     &ChatManager{},
		HookManager:     &HookManager{},
		logger:          logger,
	}
	s.initSpilloverLifecycle(store)
	t.Cleanup(func() {
		if s.spilloverLifecycleUnsub != nil {
			s.spilloverLifecycleUnsub()
		}
	})

	const key = "sess-running"
	content := strings.Repeat("u", agent.MaxResultChars+1)
	spillID, err := store.Store(key, "read", content)
	if err != nil {
		t.Fatalf("seed spill: %v", err)
	}

	// Running transition — subscriber must NOT release.
	s.sessions.ApplyLifecycleEvent(key, session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1})

	// Give the async dispatcher a reasonable window to (wrongly) fire.
	time.Sleep(300 * time.Millisecond)

	if _, err := store.Load(spillID, key); err != nil {
		t.Fatalf("spill should persist while session is running: %v", err)
	}
}

// TestSpilloverLifecycle_NilStoreIsNoop verifies we do not subscribe when the
// store hasn't been created (e.g. home dir lookup failed).
func TestSpilloverLifecycle_NilStoreIsNoop(t *testing.T) {
	s := &Server{
		ServerTransport: &ServerTransport{},
		ServerRPC:       &ServerRPC{},
		ServerRuntime:   &ServerRuntime{},
		SessionManager:  &SessionManager{sessions: session.NewManager()},
		ChatManager:     &ChatManager{},
		HookManager:     &HookManager{},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	s.initSpilloverLifecycle(nil)
	if s.spilloverLifecycleUnsub != nil {
		t.Fatal("expected no subscription when store is nil")
	}
}

// waitForSpillGone polls Load with the original session key until it errors
// (entry evicted) or the timeout elapses. Tolerates the async dispatch path.
func waitForSpillGone(store *agent.SpilloverStore, spillID, sessionKey string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := store.Load(spillID, sessionKey); err != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err := store.Load(spillID, sessionKey)
	return err != nil
}

// anyFileHasPrefix scans dir for any file whose name starts with prefix.
func anyFileHasPrefix(t *testing.T, dir, prefix string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		t.Fatalf("readdir %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			return true
		}
	}
	return false
}
