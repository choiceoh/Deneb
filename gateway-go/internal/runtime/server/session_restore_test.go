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

func TestRestoreAndWakeSessions_RestoresNativeSessions(t *testing.T) {
	// Set up a temp home dir so restoreAndWakeSessions reads from it.
	tmpHome := t.TempDir()
	transcriptDir := filepath.Join(tmpHome, ".deneb", "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)

	// Live native sessions (client:main and client:main:<id>) restore. Retired
	// shapes must NOT: the topic sessions removed in #1963 (client:coding,
	// client:topic:*) and the pre-main client:<uuid> format linger as transcript
	// files but would zombie-revive in the drawer on every SIGUSR1 restart.
	// Transients (cron) and the retired Telegram channel stay out as before.
	makeSessionTranscript(t, transcriptDir, "client:main")
	makeSessionTranscript(t, transcriptDir, "client:main:fresh-chat")
	makeSessionTranscript(t, transcriptDir, "client:coding")                               // retired topic session (#1963)
	makeSessionTranscript(t, transcriptDir, "client:topic:업무")                             // retired topic session (#1963)
	makeSessionTranscript(t, transcriptDir, "client:6ae56098-122c-40ff-a5bd-c9e6cad6faa8") // pre-main legacy format
	makeSessionTranscript(t, transcriptDir, "cron:job1")                                   // transient, not a user session
	makeSessionTranscript(t, transcriptDir, "telegram:111")                                // retired channel

	mgr := session.NewManager()
	srv := newTestServerForRestore(mgr)

	srv.restoreAndWakeSessions(context.Background())

	// Allow any goroutines spawned in safeGo to exit.
	time.Sleep(50 * time.Millisecond)

	for _, key := range []string{"client:main", "client:main:fresh-chat"} {
		if got := mgr.Get(key); got == nil {
			t.Errorf("expected %s to be restored", key)
		}
	}
	for _, key := range []string{
		"client:coding",   // retired topic session (#1963)
		"client:topic:업무", // retired topic session (#1963)
		"client:6ae56098-122c-40ff-a5bd-c9e6cad6faa8", // pre-main legacy format
		"cron:job1",    // transient
		"telegram:111", // retired channel
	} {
		if got := mgr.Get(key); got != nil {
			t.Errorf("%s should not have been restored (would zombie-revive in the drawer)", key)
		}
	}

	// Restored sessions must have DONE status and the correct channel.
	for _, key := range []string{"client:main", "client:main:fresh-chat"} {
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
}
