package server

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
)

// TestShouldReleaseCheckpoints asserts the event-kind decision table used by
// the checkpoint lifecycle subscriber. Keeping this as a pure function test
// makes the routing logic easy to reason about separately from the
// subscription plumbing.
func TestShouldReleaseCheckpoints(t *testing.T) {
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
			if got := shouldReleaseCheckpoints(tc.event); got != tc.want {
				t.Errorf("shouldReleaseCheckpoints(%+v) = %v, want %v", tc.event, got, tc.want)
			}
		})
	}
}

// TestCheckpointLifecycle_RemovesOnTerminal builds the minimum wiring needed
// to exercise the real subscription path: a session.Manager, a real
// checkpoint Manager, and the subscriber installed via initCheckpointLifecycle.
// When the session transitions to a terminal phase, the checkpoint directory
// must be removed within a short timeout — proving the hook fires and the
// removal runs end to end.
func TestCheckpointLifecycle_RemovesOnTerminal(t *testing.T) {
	root := t.TempDir()
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
	s.initCheckpointLifecycle(root)
	t.Cleanup(func() {
		if s.checkpointLifecycleUnsub != nil {
			s.checkpointLifecycleUnsub()
		}
	})

	const sessionKey = "sess-terminal"

	// Seed the checkpoint dir for this session with a real snapshot so we can
	// observe its removal.
	cpm := checkpoint.New(root, sessionKey)
	target := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if _, err := cpm.Snapshot(context.Background(), target, "fs_write"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sessionDir := cpm.SessionDir()
	if _, err := os.Stat(sessionDir); err != nil {
		t.Fatalf("session dir should exist before terminal: %v", err)
	}

	// Drive the session through start → end. The End event fires
	// EventStatusChanged with NewStatus=done, which must trigger removal.
	s.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1})
	s.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{Phase: session.PhaseEnd, Ts: 2})

	if !waitForMissing(sessionDir, 2*time.Second) {
		t.Fatalf("checkpoint dir %s still exists after terminal transition", sessionDir)
	}
}

// TestCheckpointLifecycle_RemovesOnReset verifies /reset-equivalent flow:
// ResetSession emits EventStatusChanged with NewStatus="" which should
// trigger the removal.
func TestCheckpointLifecycle_RemovesOnReset(t *testing.T) {
	root := t.TempDir()
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
	s.initCheckpointLifecycle(root)
	t.Cleanup(func() {
		if s.checkpointLifecycleUnsub != nil {
			s.checkpointLifecycleUnsub()
		}
	})

	const sessionKey = "sess-reset"

	// ResetSession requires the session to exist with a non-empty status for
	// the emitted event to carry OldStatus != NewStatus (otherwise no event).
	s.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1})

	// Snapshot after session exists so the directory is in place.
	cpm := checkpoint.New(root, sessionKey)
	target := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if _, err := cpm.Snapshot(context.Background(), target, "fs_write"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sessionDir := cpm.SessionDir()

	// Reset (simulates /reset).
	if reset := s.sessions.ResetSession(sessionKey); reset == nil {
		t.Fatal("ResetSession returned nil — precondition not met")
	}

	if !waitForMissing(sessionDir, 2*time.Second) {
		t.Fatalf("checkpoint dir %s still exists after reset", sessionDir)
	}
}

// TestCheckpointLifecycle_IgnoresRunningTransition ensures we don't
// accidentally wipe dirs for ongoing runs (EventStatusChanged → running).
func TestCheckpointLifecycle_IgnoresRunningTransition(t *testing.T) {
	root := t.TempDir()
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
	s.initCheckpointLifecycle(root)
	t.Cleanup(func() {
		if s.checkpointLifecycleUnsub != nil {
			s.checkpointLifecycleUnsub()
		}
	})

	const sessionKey = "sess-running"
	cpm := checkpoint.New(root, sessionKey)
	target := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if _, err := cpm.Snapshot(context.Background(), target, "fs_write"); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sessionDir := cpm.SessionDir()

	// Transition to running. The subscriber must NOT delete the dir.
	s.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{Phase: session.PhaseStart, Ts: 1})

	// Give the goroutine a reasonable window — if it was going to fire, it
	// would have by now (subscriber is async-dispatched).
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(sessionDir); err != nil {
		t.Fatalf("dir should persist while session is running, stat err=%v", err)
	}
}

// TestCheckpointLifecycle_EmptyRootIsNoop verifies that initCheckpointLifecycle
// does not subscribe (and therefore does not leak a subscription) when no
// storage root was configured.
func TestCheckpointLifecycle_EmptyRootIsNoop(t *testing.T) {
	s := &Server{
		ServerTransport: &ServerTransport{},
		ServerRPC:       &ServerRPC{},
		ServerRuntime:   &ServerRuntime{},
		SessionManager:  &SessionManager{sessions: session.NewManager()},
		ChatManager:     &ChatManager{},
		HookManager:     &HookManager{},
		logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	s.initCheckpointLifecycle("")
	if s.checkpointLifecycleUnsub != nil {
		t.Fatal("expected no subscription when root is empty")
	}
}

// waitForMissing polls until the path no longer exists or the timeout elapses.
// Returns true if the path is gone within the budget.
func waitForMissing(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
