package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// --- Agent-log run lifecycle pairing tests ---
//
// Regression coverage for the production bug where client:main.jsonl held 79
// run.start / run.prep entries with ZERO run.end: every native-client chat ran
// through the sync entry paths (SendSyncStream / SendSync), which logged
// start/prep inside executeAgentRun but left end/error to the async-only
// completion handlers. Orphaned starts are invisible to
// agentlog.AggregateByModel (a run is counted at its run.end), which silently
// starved the modeltuner of all interactive runs. run.end/run.error now live
// in executeAgentRun so every entry path closes the run it opened — in the
// same per-session file.

// newAgentLogSyncHandler builds a sync-test handler whose agent detail log
// writes JSONL under a fresh temp dir. Returns the handler and the log dir.
func newAgentLogSyncHandler(t *testing.T, server *httptest.Server, clientOpts ...llm.ClientOption) (*Handler, string) {
	t.Helper()
	logDir := t.TempDir()
	sm := session.NewManager()
	broadcast := func(event string, payload any) (int, []error) { return 1, nil }
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = llm.NewClient(server.URL, "test-key", clientOpts...)
	cfg.Transcript = NewMemoryTranscriptStore()
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "You are a test assistant."
	cfg.MaxTokens = 1024
	cfg.AgentLog = agentlog.NewWriter(logDir)
	h := NewHandler(sm, broadcast, logger, cfg)
	t.Cleanup(h.Close)
	return h, logDir
}

// readSessionAgentLog reads the per-session JSONL file exactly as the Writer
// lays it out on disk — the point of these tests is that start and end land in
// the SAME file, so we assert against the real file, not a reader API.
func readSessionAgentLog(t *testing.T, logDir, sessionKey string) []agentlog.LogEntry {
	t.Helper()
	f, err := os.Open(filepath.Join(logDir, sessionKey+".jsonl"))
	if err != nil {
		t.Fatalf("open session agent log: %v", err)
	}
	defer f.Close()
	var entries []agentlog.LogEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e agentlog.LogEntry
		testutil.NoError(t, json.Unmarshal(sc.Bytes(), &e))
		entries = append(entries, e)
	}
	testutil.NoError(t, sc.Err())
	return entries
}

// countByType tallies entries per log type and asserts every entry carries the
// expected session key (the Writer derives the file from entry.Session, so a
// mismatched key would mean a future write lands in a different file).
func countByType(t *testing.T, entries []agentlog.LogEntry, wantSession string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, e := range entries {
		counts[e.Type]++
		if e.Session != wantSession {
			t.Errorf("entry %s has session %q, want %q", e.Type, e.Session, wantSession)
		}
	}
	return counts
}

// runIDOf returns the runId of the single entry of the given type.
func runIDOf(t *testing.T, entries []agentlog.LogEntry, entryType string) string {
	t.Helper()
	id := ""
	for _, e := range entries {
		if e.Type != entryType {
			continue
		}
		if id != "" {
			t.Fatalf("multiple %s entries", entryType)
		}
		id = e.RunID
	}
	if id == "" {
		t.Fatalf("no %s entry", entryType)
	}
	return id
}

func newSSEOKServer(text string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseResponse(text, "end_turn"))
	}))
}

// TestAgentLog_SendSyncStream_RunEndInSameSessionFile reproduces the exact
// production shape: the native client's streaming path (stream_* run IDs in
// client:main.jsonl) must write run.end to the same file as run.start.
func TestAgentLog_SendSyncStream_RunEndInSameSessionFile(t *testing.T) {
	server := newSSEOKServer("stream reply")
	defer server.Close()
	h, logDir := newAgentLogSyncHandler(t, server)

	const sessionKey = "client:main"
	res := testutil.Must(h.SendSyncStream(context.Background(), sessionKey, "안녕", "", nil, nil))
	if res.Text == "" {
		t.Fatalf("expected non-empty reply")
	}

	entries := readSessionAgentLog(t, logDir, sessionKey)
	counts := countByType(t, entries, sessionKey)
	if counts[agentlog.TypeRunStart] != 1 || counts[agentlog.TypeRunEnd] != 1 {
		t.Fatalf("run.start=%d run.end=%d, want exactly 1 of each (counts=%v)",
			counts[agentlog.TypeRunStart], counts[agentlog.TypeRunEnd], counts)
	}
	if counts[agentlog.TypeRunError] != 0 {
		t.Fatalf("unexpected run.error in successful run (counts=%v)", counts)
	}
	startID := runIDOf(t, entries, agentlog.TypeRunStart)
	endID := runIDOf(t, entries, agentlog.TypeRunEnd)
	if startID != endID {
		t.Fatalf("run.start runId %q != run.end runId %q", startID, endID)
	}
}

// TestAgentLog_SendSync_RunEndInSameSessionFile covers the non-streaming sync
// path (miniapp.chat.send, cron single-run, heartbeat), which previously
// passed a nil RunLogger and logged nothing at all.
func TestAgentLog_SendSync_RunEndInSameSessionFile(t *testing.T) {
	server := newSSEOKServer("sync reply")
	defer server.Close()
	h, logDir := newAgentLogSyncHandler(t, server)

	const sessionKey = "client:main"
	testutil.Must(h.SendSync(context.Background(), sessionKey, "hello", "", nil))

	entries := readSessionAgentLog(t, logDir, sessionKey)
	counts := countByType(t, entries, sessionKey)
	if counts[agentlog.TypeRunStart] != 1 || counts[agentlog.TypeRunEnd] != 1 {
		t.Fatalf("run.start=%d run.end=%d, want exactly 1 of each (counts=%v)",
			counts[agentlog.TypeRunStart], counts[agentlog.TypeRunEnd], counts)
	}
	if got, want := runIDOf(t, entries, agentlog.TypeRunEnd), runIDOf(t, entries, agentlog.TypeRunStart); got != want {
		t.Fatalf("run.end runId %q != run.start runId %q", got, want)
	}
}

// TestAgentLog_SendSync_ErrorWritesRunError asserts the failure leg: an LLM
// error must close the run with run.error (not leave an orphaned start).
func TestAgentLog_SendSync_ErrorWritesRunError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error": {"message": "internal server error"}}`)
	}))
	defer server.Close()
	h, logDir := newAgentLogSyncHandler(t, server, llm.WithRetry(0, 0, 0))

	const sessionKey = "client:main"
	if _, err := h.SendSync(context.Background(), sessionKey, "hello", "", nil); err == nil {
		t.Fatalf("expected LLM error")
	}

	entries := readSessionAgentLog(t, logDir, sessionKey)
	counts := countByType(t, entries, sessionKey)
	if counts[agentlog.TypeRunStart] != 1 || counts[agentlog.TypeRunError] != 1 {
		t.Fatalf("run.start=%d run.error=%d, want exactly 1 of each (counts=%v)",
			counts[agentlog.TypeRunStart], counts[agentlog.TypeRunError], counts)
	}
	if counts[agentlog.TypeRunEnd] != 0 {
		t.Fatalf("unexpected run.end in failed run (counts=%v)", counts)
	}
	if got, want := runIDOf(t, entries, agentlog.TypeRunError), runIDOf(t, entries, agentlog.TypeRunStart); got != want {
		t.Fatalf("run.error runId %q != run.start runId %q", got, want)
	}
}

// TestAgentLog_AsyncSend_SingleRunEnd guards the relocation: run.end moved from
// handleRunSuccess into executeAgentRun, so the async path (chat.send →
// runAgentAsync) must log it exactly once — neither zero nor double.
func TestAgentLog_AsyncSend_SingleRunEnd(t *testing.T) {
	server := newSSEOKServer("async reply")
	defer server.Close()

	logDir := t.TempDir()
	sm := session.NewManager()
	bc := &broadcastCollector{}
	cfg := DefaultHandlerConfig()
	cfg.LLMClient = llm.NewClient(server.URL, "test-key")
	cfg.Transcript = NewMemoryTranscriptStore()
	cfg.DefaultModel = "test-model"
	cfg.DefaultSystem = "test"
	cfg.MaxTokens = 1024
	cfg.AgentLog = agentlog.NewWriter(logDir)
	h := NewHandler(sm, bc.broadcast, nil, cfg)
	defer h.Close()

	const sessionKey = "client:async-agentlog"
	h.Send(context.Background(), makeReq("1", "chat.send", map[string]any{
		"sessionKey":  sessionKey,
		"message":     "hello async",
		"clientRunId": "run-agentlog-1",
	}))
	if status := waitForSessionStatus(sm, sessionKey, session.StatusDone, 5*time.Second); status != session.StatusDone {
		t.Fatalf("session status = %q, want %q", status, session.StatusDone)
	}

	entries := readSessionAgentLog(t, logDir, sessionKey)
	counts := countByType(t, entries, sessionKey)
	if counts[agentlog.TypeRunStart] != 1 || counts[agentlog.TypeRunEnd] != 1 {
		t.Fatalf("run.start=%d run.end=%d, want exactly 1 of each (counts=%v)",
			counts[agentlog.TypeRunStart], counts[agentlog.TypeRunEnd], counts)
	}
}
