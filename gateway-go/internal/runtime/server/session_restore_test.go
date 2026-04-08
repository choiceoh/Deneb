package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// makeSessionTranscript writes a minimal JSONL transcript file so the restore
// scan picks it up.
func makeSessionTranscript(t *testing.T, dir, sessionKey string) {
	t.Helper()
	path := filepath.Join(dir, sessionKey+".jsonl")
	header := `{"type":"session","version":1,"id":"` + sessionKey + `","timestamp":1700000000000}` + "\n"
	if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
		t.Fatalf("makeSessionTranscript: %v", err)
	}
}

// newTestServerForRestore builds the minimal server stub needed by restoreAndWakeSessions.
func newTestServerForRestore(mgr *session.Manager) *Server {
	return &Server{
		ServerTransport:     &ServerTransport{},
		ServerRPC:           &ServerRPC{},
		ServerRuntime:       &ServerRuntime{},
		WorkflowSubsystem:   &WorkflowSubsystem{},
		MemorySubsystem:     &MemorySubsystem{},
		AutonomousSubsystem: &AutonomousSubsystem{},
		InfraSubsystem:      &InfraSubsystem{},
		SessionManager:      &SessionManager{sessions: mgr},
		ChatManager:         &ChatManager{},
		HookManager:         &HookManager{},
		logger:              slog.Default(),
	}
}

func TestRestoreAndWakeSessions_RestoresTelegramSessions(t *testing.T) {
	// Set up a temp home dir so restoreAndWakeSessions reads from it.
	tmpHome := t.TempDir()
	transcriptDir := filepath.Join(tmpHome, ".deneb", "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)

	// Create main telegram transcripts, a sub-session, and a non-telegram transcript.
	makeSessionTranscript(t, transcriptDir, "telegram:111")
	makeSessionTranscript(t, transcriptDir, "telegram:222")
	makeSessionTranscript(t, transcriptDir, "telegram:111:some-task:1234567890") // sub-session, should not be restored
	makeSessionTranscript(t, transcriptDir, "cron:job1")                         // should not be restored

	mgr := session.NewManager()
	srv := newTestServerForRestore(mgr)

	srv.restoreAndWakeSessions(context.Background())

	// Allow any goroutines spawned in safeGo to exit.
	time.Sleep(50 * time.Millisecond)

	if got := mgr.Get("telegram:111"); got == nil {
		t.Error("expected telegram:111 to be restored")
	}
	if got := mgr.Get("telegram:222"); got == nil {
		t.Error("expected telegram:222 to be restored")
	}
	if got := mgr.Get("cron:job1"); got != nil {
		t.Error("cron:job1 should not have been restored")
	}
	if got := mgr.Get("telegram:111:some-task:1234567890"); got != nil {
		t.Error("sub-session telegram:111:some-task:1234567890 should not have been restored")
	}

	// Restored sessions must have DONE status and be from the telegram channel.
	for _, key := range []string{"telegram:111", "telegram:222"} {
		s := mgr.Get(key)
		if s == nil {
			continue
		}
		if s.Status != session.StatusDone {
			t.Errorf("%s: got %q, want status DONE", key, s.Status)
		}
		if s.Channel != "telegram" {
			t.Errorf("%s: got %q, want channel telegram", key, s.Channel)
		}
	}
}

func TestRestoreAndWakeSessions_SkipsAlreadyRestoredSessions(t *testing.T) {
	tmpHome := t.TempDir()
	transcriptDir := filepath.Join(tmpHome, ".deneb", "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)

	makeSessionTranscript(t, transcriptDir, "telegram:999")

	mgr := session.NewManager()
	// Pre-populate the manager so the session is already in memory.
	_ = mgr.Set(&session.Session{
		Key:     "telegram:999",
		Kind:    session.KindDirect,
		Status:  session.StatusRunning,
		Channel: "telegram",
	})

	srv := newTestServerForRestore(mgr)
	srv.restoreAndWakeSessions(context.Background())
	time.Sleep(50 * time.Millisecond)

	// Status should remain running — not overwritten to done.
	if got := mgr.Get("telegram:999"); got == nil || got.Status != session.StatusRunning {
		t.Error("existing session status should not have been overwritten")
	}
}

func TestRestoreAndWakeSessions_NoTranscriptDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// ~/.deneb/transcripts does not exist — should be a no-op, not a panic.

	mgr := session.NewManager()
	srv := newTestServerForRestore(mgr)

	// Must not panic.
	srv.restoreAndWakeSessions(context.Background())

	if count := mgr.Count(); count != 0 {
		t.Errorf("got %d, want 0 sessions", count)
	}
}
