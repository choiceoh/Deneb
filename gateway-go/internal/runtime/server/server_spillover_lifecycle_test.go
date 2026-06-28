package server

import (
	"io"
	"log/slog"
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
		{"reset (empty status) releases", session.Event{Kind: session.EventStatusChanged, OldStatus: session.StatusDone, NewStatus: ""}, true},
		{"empty status without old status does NOT release", session.Event{Kind: session.EventStatusChanged, NewStatus: ""}, false},
		{"status → done does NOT release", session.Event{Kind: session.EventStatusChanged, OldStatus: session.StatusRunning, NewStatus: session.StatusDone}, false},
		{"status → failed does NOT release", session.Event{Kind: session.EventStatusChanged, OldStatus: session.StatusRunning, NewStatus: session.StatusFailed}, false},
		{"status → killed does NOT release", session.Event{Kind: session.EventStatusChanged, OldStatus: session.StatusRunning, NewStatus: session.StatusKilled}, false},
		{"status → timeout does NOT release", session.Event{Kind: session.EventStatusChanged, OldStatus: session.StatusRunning, NewStatus: session.StatusTimeout}, false},
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

// TestSpilloverLifecycle_PreservesOnTerminal verifies the event-driven
// lifecycle hook does not treat an ordinary completed turn as teardown.
// finishRun handles the common completion path separately; this subscriber is
// reserved for reset/delete flows.
func TestSpilloverLifecycle_PreservesOnTerminal(t *testing.T) {
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

	const key = "sess-done"
	content := strings.Repeat("x", agent.MaxResultChars+1)
	spillID, err := store.Store(key, "read", content)
	if err != nil {
		t.Fatalf("seed spill: %v", err)
	}

	// Drive the session through start → end; the subscriber must not remove it.
	s.sessions.ApplyLifecycleEvent(key, session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1})
	s.sessions.ApplyLifecycleEvent(key, session.LifecycleEvent{Phase: session.PhaseEnd, Ts: 2})

	time.Sleep(300 * time.Millisecond)
	if _, err := store.Load(spillID, key); err != nil {
		t.Fatalf("spill should persist after terminal transition: %v", err)
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

// TestSpilloverLifecycle_IgnoresPatchEvents verifies non-reset status-change
// emissions from sessions.patch/configureCoding do not wipe spillover.
func TestSpilloverLifecycle_IgnoresPatchEvents(t *testing.T) {
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

	const key = "sess-patch"
	content := strings.Repeat("p", agent.MaxResultChars+1)
	spillID, err := store.Store(key, "read", content)
	if err != nil {
		t.Fatalf("seed spill: %v", err)
	}

	label := "patched"
	s.sessions.Patch(key, session.PatchFields{Label: &label})

	time.Sleep(300 * time.Millisecond)
	if _, err := store.Load(spillID, key); err != nil {
		t.Fatalf("spill should persist after patch event: %v", err)
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
