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

func TestRestoreAndWakeSessions_RestoresNativeAndLegacyTelegramSessions(t *testing.T) {
	// Set up a temp home dir so restoreAndWakeSessions reads from it.
	tmpHome := t.TempDir()
	transcriptDir := filepath.Join(tmpHome, ".deneb", "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)

	// Create native transcripts, legacy Telegram transcripts, and transient
	// sessions that must stay out of the restored user session list.
	makeSessionTranscript(t, transcriptDir, "client:main")
	makeSessionTranscript(t, transcriptDir, "client:coding")
	makeSessionTranscript(t, transcriptDir, "client:main:fresh-chat")
	makeSessionTranscript(t, transcriptDir, "telegram:111")
	makeSessionTranscript(t, transcriptDir, "telegram:222")
	makeSessionTranscript(t, transcriptDir, "telegram:111:some-task:1234567890") // sub-session, should not be restored
	makeSessionTranscript(t, transcriptDir, "cron:job1")                         // should not be restored

	mgr := session.NewManager()
	srv := newTestServerForRestore(mgr)

	srv.restoreAndWakeSessions(context.Background())

	// Allow any goroutines spawned in safeGo to exit.
	time.Sleep(50 * time.Millisecond)

	for _, key := range []string{"client:main", "client:coding", "client:main:fresh-chat"} {
		if got := mgr.Get(key); got == nil {
			t.Errorf("expected %s to be restored", key)
		}
	}
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

	// Restored sessions must have DONE status and the correct channel.
	for _, key := range []string{"client:main", "client:coding", "client:main:fresh-chat"} {
		s := mgr.Get(key)
		if s == nil {
			continue
		}
		if s.Status != session.StatusDone {
			t.Errorf("%s: got %q, want status DONE", key, s.Status)
		}
		if s.Channel != "client" {
			t.Errorf("%s: got %q, want channel client", key, s.Channel)
		}
	}
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
