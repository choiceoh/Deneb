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
			sessionKey: "client:empty",
			lines:      nil,
			want:       tailEmpty,
		},
		{
			name:       "user text (LLM never replied)",
			sessionKey: "client:user_text",
			lines: []string{
				`{"role":"user","content":"hi","timestamp":1700000001000}`,
			},
			want: tailEndUserText,
		},
		{
			name:       "assistant text only (clean end)",
			sessionKey: "client:asst_text",
			lines: []string{
				`{"role":"user","content":"hi","timestamp":1700000001000}`,
				`{"role":"assistant","content":[{"type":"text","text":"hello"}],"timestamp":1700000001500}`,
			},
			want: tailEndAssistantText,
		},
		{
			name:       "assistant tool_use (interrupted mid-tool)",
			sessionKey: "client:asst_tool",
			lines: []string{
				`{"role":"user","content":"do it","timestamp":1700000001000}`,
				`{"role":"assistant","content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"t1","name":"fs","input":{}}],"timestamp":1700000001500}`,
			},
			want: tailEndAssistantToolUse,
		},
		{
			name:       "tool_result (next turn never started)",
			sessionKey: "client:tool_result",
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
	key := "client:torn"
	dir := filepath.Join(tmpHome, ".deneb", "transcripts")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, key+".jsonl")
	content := `{"type":"session","version":1,"id":"client:torn","timestamp":1}` + "\n" +
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

// fakeDispatcher captures resume dispatches for assertion.
type fakeDispatcher struct {
	mu    sync.Mutex
	calls []dispatchCall
}

type dispatchCall struct {
	SessionKey string
	Channel    string
	To         string
}

func (f *fakeDispatcher) fn(ctx context.Context, sessionKey, channel, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, dispatchCall{SessionKey: sessionKey, Channel: channel, To: to})
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

// Test: a fresh native client marker + user-text tail → resume fires without
// Telegram delivery reconstruction.
func TestAutoResume_ResumesInterruptedNativeClientTurn(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	sessionKey := "client:main:fresh-chat"
	writeTranscriptLine(t, tmpHome, sessionKey,
		`{"role":"user","content":"이어 해줘","timestamp":1700000001000}`,
	)
	store := srv.runMarkerStore()
	if err := store.Write(session.RunMarker{
		SessionKey:     sessionKey,
		StartedAt:      time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(),
		Channel:        "client",
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

	waitForCondition(t, 5*time.Second, func() bool { return fd.count() == 1 })

	calls := fd.snapshot()
	if calls[0].SessionKey != sessionKey || calls[0].Channel != "client" || calls[0].To != "" {
		t.Errorf("unexpected dispatch: %+v", calls[0])
	}

	m, _ := store.Read(sessionKey)
	if m == nil || m.ResumeAttempts != 1 {
		t.Errorf("expected attempts=1 on persisted marker, got %+v", m)
	}
}

// Test: assistant-text tail is logically done — no resume.
func TestAutoResume_SkipsCleanEnd(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	writeTranscriptLine(t, tmpHome, "client:7",
		`{"role":"user","content":"hi","timestamp":1700000001000}`,
		`{"role":"assistant","content":[{"type":"text","text":"hello"}],"timestamp":1700000001500}`,
	)
	store := srv.runMarkerStore()
	_ = store.Write(session.RunMarker{
		SessionKey: "client:7", StartedAt: time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(), Channel: "client",
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: true, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	// The skip decision is synchronous: the call deletes the marker inline and
	// never launches a dispatch goroutine, so the outcome is final on return —
	// no blind sleep needed. A regression that wrongly dispatched would leave
	// the marker present (attempts incremented, not deleted), which the marker
	// assertion below catches deterministically.
	if fd.count() != 0 {
		t.Errorf("expected no dispatch, got %d calls", fd.count())
	}
	if m, _ := store.Read("client:7"); m != nil {
		t.Errorf("expected marker to be deleted, got %+v", m)
	}
}

// Test: marker older than max age is discarded without resuming.
func TestAutoResume_DiscardsStaleMarker(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	writeTranscriptLine(t, tmpHome, "client:stale",
		`{"role":"user","content":"old","timestamp":1}`,
	)
	store := srv.runMarkerStore()
	// StartedAt = 10 hours ago (older than 2h default MaxAge).
	stale := time.Now().Add(-10 * time.Hour).UnixMilli()
	_ = store.Write(session.RunMarker{
		SessionKey: "client:stale", StartedAt: stale,
		LastActivityAt: stale, Channel: "client",
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: true, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	// Synchronous skip (see TestAutoResume_SkipsCleanEnd): no goroutine is
	// launched for a stale marker, so assert immediately.
	if fd.count() != 0 {
		t.Errorf("expected no dispatch for stale marker, got %d", fd.count())
	}
	if m, _ := store.Read("client:stale"); m != nil {
		t.Errorf("expected stale marker to be deleted, got %+v", m)
	}
}

// Test: already-resumed marker (attempts >= limit) does not re-resume.
func TestAutoResume_RespectsAttemptLimit(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	writeTranscriptLine(t, tmpHome, "client:loop",
		`{"role":"user","content":"retry bait","timestamp":1}`,
	)
	store := srv.runMarkerStore()
	_ = store.Write(session.RunMarker{
		SessionKey: "client:loop", StartedAt: time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(), Channel: "client",
		ResumeAttempts: 1,
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: true, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	// Synchronous skip (see TestAutoResume_SkipsCleanEnd): no goroutine is
	// launched once the attempt limit is hit, so assert immediately.
	if fd.count() != 0 {
		t.Errorf("expected no dispatch when attempts exhausted, got %d", fd.count())
	}
	if m, _ := store.Read("client:loop"); m != nil {
		t.Errorf("expected exhausted marker to be deleted, got %+v", m)
	}
}

// Test: disabled via Enabled=false — no dispatch, and markers are cleared.
func TestAutoResume_DisabledDrainsMarkers(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	writeTranscriptLine(t, tmpHome, "client:1",
		`{"role":"user","content":"a","timestamp":1}`,
	)
	store := srv.runMarkerStore()
	_ = store.Write(session.RunMarker{
		SessionKey: "client:1", StartedAt: time.Now().UnixMilli(),
		LastActivityAt: time.Now().UnixMilli(), Channel: "client",
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: false, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	// The disabled path returns synchronously after draining markers and never
	// dispatches, so assert immediately (no blind sleep).
	if fd.count() != 0 {
		t.Errorf("expected no dispatch when disabled, got %d", fd.count())
	}
	// Markers are proactively cleared even when the feature is off, so they
	// cannot accumulate to infinity.
	if m, _ := store.Read("client:1"); m != nil {
		t.Errorf("expected marker to be cleared on disabled path, got %+v", m)
	}
}

// Test: non-user session keys (cron, btw) are skipped.
func TestAutoResume_SkipsNonUserSessions(t *testing.T) {
	tmpHome := t.TempDir()
	srv := newAutoResumeTestServer(t, tmpHome)

	store := srv.runMarkerStore()
	_ = store.Write(session.RunMarker{
		SessionKey: "cron:nightly", StartedAt: time.Now().UnixMilli(),
		Channel: "cron",
	})
	_ = store.Write(session.RunMarker{
		SessionKey: "btw:abc", StartedAt: time.Now().UnixMilli(),
		Channel: "client",
	})

	fd := &fakeDispatcher{}
	opts := autoResumeOptions{
		Enabled: true, MaxAge: resumeMaxAge, MaxAttempts: 1,
		Now: time.Now, DispatchFn: fd.fn,
	}
	srv.autoResumeInterruptedRunsWithOpts(context.Background(), opts)

	// Non-resumable sessions are skipped and their markers deleted synchronously
	// — no goroutine launched — so assert immediately. The marker-deletion
	// checks are the deterministic proof the skip path (not dispatch) ran.
	if fd.count() != 0 {
		t.Errorf("expected no dispatch for non-user sessions, got %d", fd.count())
	}
	if m, _ := store.Read("cron:nightly"); m != nil {
		t.Errorf("expected cron marker to be deleted, got %+v", m)
	}
	if m, _ := store.Read("btw:abc"); m != nil {
		t.Errorf("expected btw marker to be deleted, got %+v", m)
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
	sm.Create("client:99", session.KindDirect)
	if err := sm.Set(&session.Session{
		Key: "client:99", Kind: session.KindDirect, Channel: "client",
		Status: session.StatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	store := srv.runMarkerStore()
	// Event dispatch is async — poll for up to 2s.
	var marker *session.RunMarker
	waitForCondition(t, 2*time.Second, func() bool {
		m, _ := store.Read("client:99")
		marker = m
		return m != nil
	})
	if marker == nil {
		t.Fatal("marker not written on StatusRunning")
	}
	if marker.Channel != "client" {
		t.Errorf("marker channel = %q want client", marker.Channel)
	}

	// Terminal transition clears the marker.
	if err := sm.Set(&session.Session{
		Key: "client:99", Kind: session.KindDirect, Channel: "client",
		Status: session.StatusDone,
	}); err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		m, _ := store.Read("client:99")
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
	store := srv.runMarkerStore()

	// Non-direct kind: the listener must NOT write a marker for it.
	sm.Create("cron:job1", session.KindCron)
	if err := sm.Set(&session.Session{
		Key: "cron:job1", Kind: session.KindCron,
		Status: session.StatusRunning,
	}); err != nil {
		t.Fatal(err)
	}

	// Sentinel: a direct session enqueued AFTER the cron event. The marker
	// listener drains its mailbox FIFO in a single goroutine (session.EventBus),
	// so once the sentinel's marker appears the earlier cron event has already
	// been processed. That ordering is a deterministic sync point — it replaces
	// the old blind sleep, which false-passed if the (unwanted) write was merely
	// slow rather than absent.
	sm.Create("client:sentinel", session.KindDirect)
	if err := sm.Set(&session.Session{
		Key: "client:sentinel", Kind: session.KindDirect, Channel: "client",
		Status: session.StatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	waitForCondition(t, 2*time.Second, func() bool {
		m, _ := store.Read("client:sentinel")
		return m != nil
	})

	if m, _ := store.Read("cron:job1"); m != nil {
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
