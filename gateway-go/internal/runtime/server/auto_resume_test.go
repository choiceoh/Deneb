package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// writeTranscriptLine appends one JSON line to a transcript file under tmpHome.
// The session header is added lazily on the first call per key.
func writeTranscriptLine(t *testing.T, tmpHome, sessionKey string, lines ...string) {
	t.Helper()
	dir := filepath.Join(tmpHome, ".deneb", "transcripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sessionKey+".jsonl")
	// Lazy header.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		header := `{"type":"session","version":1,"id":"` + sessionKey + `","timestamp":1700000000000}` + "\n"
		if err := os.WriteFile(path, []byte(header), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

// Test: analyzeTranscriptTail classifies every shape correctly.
func TestAnalyzeTranscriptTail_AllShapes(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()

	tests := []struct {
		name       string
		sessionKey string
		lines      []string
		want       transcriptTailShape
	}{
		{
			name:       "empty (just header)",
			sessionKey: "telegram:empty",
			lines:      nil,
			want:       tailEmpty,
		},
		{
			name:       "user text (LLM never replied)",
			sessionKey: "telegram:user_text",
			lines: []string{
				`{"role":"user","content":"hi","timestamp":1700000001000}`,
			},
			want: tailEndUserText,
		},
		{
			name:       "assistant text only (clean end)",
			sessionKey: "telegram:asst_text",
			lines: []string{
				`{"role":"user","content":"hi","timestamp":1700000001000}`,
				`{"role":"assistant","content":[{"type":"text","text":"hello"}],"timestamp":1700000001500}`,
			},
			want: tailEndAssistantText,
		},
		{
			name:       "assistant tool_use (interrupted mid-tool)",
			sessionKey: "telegram:asst_tool",
			lines: []string{
				`{"role":"user","content":"do it","timestamp":1700000001000}`,
				`{"role":"assistant","content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"t1","name":"fs","input":{}}],"timestamp":1700000001500}`,
			},
			want: tailEndAssistantToolUse,
		},
		{
			name:       "tool_result (next turn never started)",
			sessionKey: "telegram:tool_result",
			lines: []string{
				`{"role":"user","content":"do it","timestamp":1700000001000}`,
				`{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"fs","input":{}}],"timestamp":1700000001500}`,
				`{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}],"timestamp":1700000002000}`,
			},
			want: tailEndToolResult,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if len(tc.lines) > 0 {
				writeTranscriptLine(t, tmpHome, tc.sessionKey, tc.lines...)
			} else {
				// Header-only file.
				dir := filepath.Join(tmpHome, ".deneb", "transcripts")
				_ = os.MkdirAll(dir, 0o755)
				path := filepath.Join(dir, tc.sessionKey+".jsonl")
				header := `{"type":"session","version":1,"id":"` + tc.sessionKey + `","timestamp":1}` + "\n"
				_ = os.WriteFile(path, []byte(header), 0o644)
			}
			path := filepath.Join(tmpHome, ".deneb", "transcripts", tc.sessionKey+".jsonl")
			shape, err := analyzeTranscriptTail(path)
			if err != nil {
				t.Fatalf("analyzeTranscriptTail: %v", err)
			}
			if shape != tc.want {
				t.Errorf("got %s want %s", tailShapeString(shape), tailShapeString(tc.want))
			}
		})
	}
}

// Test: a partial/corrupt line at the tail is tolerated.
func TestAnalyzeTranscriptTail_TolerantOfTrailingJunk(t *testing.T) {
	t.Parallel()
	tmpHome := t.TempDir()
	key := "telegram:torn"
	dir := filepath.Join(tmpHome, ".deneb", "transcripts")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, key+".jsonl")
	content := `{"type":"session","version":1,"id":"telegram:torn","timestamp":1}` + "\n" +
		`{"role":"user","content":"hi","timestamp":1700000001000}` + "\n" +
		`{"role":"assistant","content":[{"type":"tex`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	shape, err := analyzeTranscriptTail(path)
	if err != nil {
		t.Fatalf("analyzeTranscriptTail: %v", err)
	}
	// Torn trailing line is ignored; the last complete message was the user.
	if shape != tailEndUserText {
		t.Errorf("got %s want %s", tailShapeString(shape), tailShapeString(tailEndUserText))
	}
}

// Test: parseTelegramChatID.
func TestParseTelegramChatID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key  string
		want string
		ok   bool
	}{
		{"telegram:42", "42", true},
		{"telegram:7074071666", "7074071666", true},
		{"telegram:42:some-task:1234567890", "", false}, // sub-session
		{"cron:job1", "", false},
		{"btw:abc", "", false},
		{"", "", false},
		{"telegram:", "", false},
	}
	for _, tc := range cases {
		got, ok := parseTelegramChatID(tc.key)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseTelegramChatID(%q): got (%q,%v) want (%q,%v)",
				tc.key, got, ok, tc.want, tc.ok)
		}
	}
}

// fakeDispatcher captures resume dispatches for assertion.
type fakeDispatcher struct {
	mu    sync.Mutex
	calls []dispatchCall
}

type dispatchCall struct {
	SessionKey string
	Channel    string
	ChatID     string
}

func (f *fakeDispatcher) fn(ctx context.Context, sessionKey, channel, chatID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, dispatchCall{SessionKey: sessionKey, Channel: channel, ChatID: chatID})
	return nil
}

func (f *fakeDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeDispatcher) snapshot() []dispatchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]dispatchCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// newAutoResumeTestServer builds a Server stub with just the fields the
// auto-resume path touches. denebDir is rooted at tmpHome so the marker
// store and transcript dir are both hermetic.
func newAutoResumeTestServer(t *testing.T, tmpHome string) *Server {
	t.Helper()
	// t.Setenv requires the test not be marked t.Parallel() — callers of
	// this helper must omit t.Parallel().
	t.Setenv("HOME", tmpHome)

	denebDir := filepath.Join(tmpHome, ".deneb")
	_ = os.MkdirAll(denebDir, 0o755)

	srv := &Server{
		ServerTransport:     &ServerTransport{},
		ServerRPC:           &ServerRPC{},
		ServerRuntime:       &ServerRuntime{},
		WorkflowSubsystem:   &WorkflowSubsystem{},
		MemorySubsystem:     &MemorySubsystem{},
		AutonomousSubsystem: &AutonomousSubsystem{},
		InfraSubsystem:      &InfraSubsystem{},
		SessionManager:      &SessionManager{sessions: session.NewManager()},
		ChatManager:         &ChatManager{},
		HookManager:         &HookManager{},
		logger:              slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		denebDir:            denebDir,
	}
	return srv
}

// waitForCondition polls fn until it returns true or the deadline fires.
func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// Test: a fresh marker + user-text tail → resume fires.
func TestAutoResume_ResumesInterruptedUserTurn(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	// Seed: user wrote a message, LLM never responded, gateway crashed.
	writeTranscriptLine(t, tmpHome, "telegram:42",
		`{"role":"user","content":"안녕","timestamp":1700000001000}`,
	)
	store := srv.runMarkerStore()
	if err := store.Write(session.RunMarker{
		SessionKey:     "telegram:42",
		StartedAt:      time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(),
		Channel:        "telegram",
	}); err != nil {
		t.Fatal(err)
	}

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled:     true,
		MaxAge:      resumeMaxAge,
		MaxAttempts: 1,
		Now:         time.Now,
		DispatchFn:  fd.fn,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.autoResumeInterruptedRunsWithOpts(ctx, opts)

	// Dispatch happens after resumeDispatchDelay in a safego — wait.
	waitForCondition(t, 5*time.Second, func() bool { return fd.count() == 1 })

	calls := fd.snapshot()
	if calls[0].SessionKey != "telegram:42" || calls[0].ChatID != "42" || calls[0].Channel != "telegram" {
		t.Errorf("unexpected dispatch: %+v", calls[0])
	}

	// Marker should now have ResumeAttempts==1 (bumped BEFORE dispatch fires).
	m, _ := store.Read("telegram:42")
	if m == nil || m.ResumeAttempts != 1 {
		t.Errorf("expected attempts=1 on persisted marker, got %+v", m)
	}
}

// Test: assistant-text tail is logically done — no resume.
func TestAutoResume_SkipsCleanEnd(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	writeTranscriptLine(t, tmpHome, "telegram:7",
		`{"role":"user","content":"hi","timestamp":1700000001000}`,
		`{"role":"assistant","content":[{"type":"text","text":"hello"}],"timestamp":1700000001500}`,
	)
	store := srv.runMarkerStore()
	_ = store.Write(session.RunMarker{
		SessionKey: "telegram:7", StartedAt: time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(), Channel: "telegram",
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: true, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	// Give any (unexpected) goroutine a chance to run.
	time.Sleep(200 * time.Millisecond)
	if fd.count() != 0 {
		t.Errorf("expected no dispatch, got %d calls", fd.count())
	}
	// Marker should be deleted.
	m, _ := store.Read("telegram:7")
	if m != nil {
		t.Errorf("expected marker to be deleted, got %+v", m)
	}
}

// Test: marker older than max age is discarded without resuming.
func TestAutoResume_DiscardsStaleMarker(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	writeTranscriptLine(t, tmpHome, "telegram:stale",
		`{"role":"user","content":"old","timestamp":1}`,
	)
	store := srv.runMarkerStore()
	// StartedAt = 10 hours ago (older than 2h default MaxAge).
	stale := time.Now().Add(-10 * time.Hour).UnixMilli()
	_ = store.Write(session.RunMarker{
		SessionKey: "telegram:stale", StartedAt: stale,
		LastActivityAt: stale, Channel: "telegram",
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: true, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	time.Sleep(100 * time.Millisecond)
	if fd.count() != 0 {
		t.Errorf("expected no dispatch for stale marker, got %d", fd.count())
	}
	// Stale markers get cleaned up.
	m, _ := store.Read("telegram:stale")
	if m != nil {
		t.Errorf("expected stale marker to be deleted, got %+v", m)
	}
}

// Test: already-resumed marker (attempts >= limit) does not re-resume.
func TestAutoResume_RespectsAttemptLimit(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	writeTranscriptLine(t, tmpHome, "telegram:loop",
		`{"role":"user","content":"retry bait","timestamp":1}`,
	)
	store := srv.runMarkerStore()
	_ = store.Write(session.RunMarker{
		SessionKey: "telegram:loop", StartedAt: time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(), Channel: "telegram",
		ResumeAttempts: 1,
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: true, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	time.Sleep(100 * time.Millisecond)
	if fd.count() != 0 {
		t.Errorf("expected no dispatch when attempts exhausted, got %d", fd.count())
	}
	// Marker is cleared so it does not show up on future boots.
	m, _ := store.Read("telegram:loop")
	if m != nil {
		t.Errorf("expected exhausted marker to be deleted, got %+v", m)
	}
}

// Test: disabled via Enabled=false — no dispatch, and markers are cleared.
func TestAutoResume_DisabledDrainsMarkers(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	writeTranscriptLine(t, tmpHome, "telegram:1",
		`{"role":"user","content":"a","timestamp":1}`,
	)
	store := srv.runMarkerStore()
	_ = store.Write(session.RunMarker{
		SessionKey: "telegram:1", StartedAt: time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(), Channel: "telegram",
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: false, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	time.Sleep(100 * time.Millisecond)
	if fd.count() != 0 {
		t.Errorf("expected no dispatch when disabled, got %d", fd.count())
	}
	// Markers are proactively cleared even when the feature is off, so they
	// cannot accumulate to infinity.
	m, _ := store.Read("telegram:1")
	if m != nil {
		t.Errorf("expected marker to be cleared on disabled path, got %+v", m)
	}
}

// Test: non-telegram session keys (cron, btw) are skipped.
func TestAutoResume_SkipsNonTelegramSessions(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	store := srv.runMarkerStore()
	_ = store.Write(session.RunMarker{
		SessionKey: "cron:nightly", StartedAt: time.Now().UnixMilli(),
		Channel: "cron",
	})
	_ = store.Write(session.RunMarker{
		SessionKey: "btw:abc", StartedAt: time.Now().UnixMilli(),
		Channel: "telegram",
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: true, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	time.Sleep(100 * time.Millisecond)
	if fd.count() != 0 {
		t.Errorf("expected no dispatch for non-telegram sessions, got %d", fd.count())
	}
}

// Test: lifecycle listener writes markers on Running and deletes on terminal.
func TestRunMarkerLifecycle_WriteOnRunningDeleteOnTerminal(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)
	unsub := srv.initRunMarkerLifecycle()
	defer unsub()

	sm := srv.sessions
	// Create a direct session, transition to running.
	sm.Create("telegram:99", session.KindDirect)
	if err := sm.Set(&session.Session{
		Key: "telegram:99", Kind: session.KindDirect, Channel: "telegram",
		Status: session.StatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	store := srv.runMarkerStore()
	// Event dispatch is async — poll for up to 2s.
	var marker *session.RunMarker
	waitForCondition(t, 2*time.Second, func() bool {
		m, _ := store.Read("telegram:99")
		marker = m
		return m != nil
	})
	if marker == nil {
		t.Fatal("marker not written on StatusRunning")
	}
	if marker.Channel != "telegram" {
		t.Errorf("marker channel = %q want telegram", marker.Channel)
	}

	// Terminal transition clears the marker.
	if err := sm.Set(&session.Session{
		Key: "telegram:99", Kind: session.KindDirect, Channel: "telegram",
		Status: session.StatusDone,
	}); err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		m, _ := store.Read("telegram:99")
		return m == nil
	})
}

// Test: lifecycle listener skips non-direct kinds (cron/subagent do not
// need markers — those have their own retry paths).
func TestRunMarkerLifecycle_SkipsNonDirectKinds(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)
	unsub := srv.initRunMarkerLifecycle()
	defer unsub()

	sm := srv.sessions
	sm.Create("cron:job1", session.KindCron)
	if err := sm.Set(&session.Session{
		Key: "cron:job1", Kind: session.KindCron,
		Status: session.StatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	// Give the async event bus a chance to fire.
	time.Sleep(300 * time.Millisecond)
	store := srv.runMarkerStore()
	m, _ := store.Read("cron:job1")
	if m != nil {
		t.Errorf("expected no marker for cron session, got %+v", m)
	}
}

// Test: autoResumeEnabled parses config correctly.
func TestAutoResumeEnabled_DefaultWhenMissing(t *testing.T) {
	// Point HOME at an empty dir — no deneb.json, so default applies.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if !autoResumeEnabled() {
		t.Errorf("expected true when config missing")
	}
}

func TestAutoResumeEnabled_ExplicitFalse(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	denebDir := filepath.Join(tmp, ".deneb")
	_ = os.MkdirAll(denebDir, 0o755)

	falseVal := false
	cfg := map[string]any{
		"session": map[string]any{"autoResume": falseVal},
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(denebDir, "deneb.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if autoResumeEnabled() {
		t.Errorf("expected false when config sets autoResume=false")
	}
}

// Dispatch counter assertion uses atomics too, so the test is race-safe
// when we run with -race.
var _ = atomic.Int32{}
