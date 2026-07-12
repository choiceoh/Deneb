package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func decodeSessionPayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	rpctest.MustOK(t, resp)
	var got T
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, resp.Payload)
	}
	return got
}

type execChatStub struct {
	method string
}

func (s *execChatStub) ChatReady() bool { return s != nil }

func (s *execChatStub) SessionsSend(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	s.method = "send"
	return rpcutil.RespondOK(req.ID, map[string]bool{"ok": true})
}

func (s *execChatStub) SessionsSteer(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	s.method = "steer"
	return rpcutil.RespondOK(req.ID, map[string]bool{"ok": true})
}

func (s *execChatStub) SessionsAbort(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	s.method = "abort"
	return rpcutil.RespondOK(req.ID, map[string]bool{"ok": true})
}

func TestMethodsAndCRUDMethodsExposeStableSurfaces(t *testing.T) {
	methods := Methods(Deps{})
	wantMethods := []string{"sessions.patch", "sessions.reset", "sessions.overflow_check"}
	for _, name := range wantMethods {
		if methods[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	if len(methods) != len(wantMethods) {
		t.Fatalf("Methods count = %d", len(methods))
	}
	crud := CRUDMethods(Deps{})
	wantCRUD := []string{"sessions.list", "sessions.get", "sessions.delete"}
	for _, name := range wantCRUD {
		if crud[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	if len(crud) != len(wantCRUD) {
		t.Fatalf("CRUDMethods count = %d", len(crud))
	}
	if got := ExecMethods(ExecDeps{}); got != nil {
		t.Fatalf("ExecMethods without chat = %#v", got)
	}
}

func TestExecMethodsRejectTypedNilAndDelegateThroughPort(t *testing.T) {
	var typedNil *execChatStub
	if got := ExecMethods(ExecDeps{Chat: typedNil}); got != nil {
		t.Fatalf("ExecMethods with typed nil chat = %#v", got)
	}

	chat := &execChatStub{}
	methods := ExecMethods(ExecDeps{Chat: chat})
	for method, want := range map[string]string{
		"sessions.send":  "send",
		"sessions.steer": "steer",
		"sessions.abort": "abort",
		"agent":          "send",
	} {
		resp := methods[method](context.Background(), &protocol.RequestFrame{ID: method})
		if resp == nil || resp.Error != nil || chat.method != want {
			t.Fatalf("%s delegated as %q, response=%#v", method, chat.method, resp)
		}
	}
}

func TestSessionsPatchCreatesTrimsAndAppliesEveryField(t *testing.T) {
	mgr := runtimesession.NewManager()
	methods := Methods(Deps{Sessions: mgr})
	resp := rpctest.Call(methods, "sessions.patch", map[string]any{
		"key":                  "  client:main:test  ",
		"label":                "Test Session",
		"model":                "model-x",
		"thinkingLevel":        "high",
		"interleavedThinking":  true,
		"showThinkingInChat":   true,
		"fastMode":             false,
		"verboseLevel":         "full",
		"reasoningLevel":       "medium",
		"elevatedLevel":        "ask",
		"execHost":             "sandbox",
		"execSecurity":         "allowlist",
		"execAsk":              "on-miss",
		"responseUsage":        "full",
		"spawnedBy":            "parent",
		"spawnedWorkspaceDir":  "/workspace",
		"spawnDepth":           2,
		"subagentRole":         "reviewer",
		"subagentControlScope": "children",
	})
	type payload struct {
		OK    bool                    `json:"ok"`
		Key   string                  `json:"key"`
		Entry *runtimesession.Session `json:"entry"`
	}
	got := decodeSessionPayload[payload](t, resp)
	if !got.OK || got.Key != "client:main:test" || got.Entry == nil {
		t.Fatalf("patch payload = %#v", got)
	}
	e := got.Entry
	if e.Key != got.Key || e.Kind != runtimesession.KindUnknown || e.Label != "Test Session" || e.Model != "model-x" ||
		e.ThinkingLevel != "high" || e.InterleavedThinking == nil || !*e.InterleavedThinking ||
		e.ShowThinkingInChat == nil || !*e.ShowThinkingInChat || e.FastMode == nil || *e.FastMode ||
		e.VerboseLevel != "full" || e.ReasoningLevel != "medium" || e.ElevatedLevel != "ask" ||
		e.ExecHost != "sandbox" || e.ExecSecurity != "allowlist" || e.ExecAsk != "on-miss" ||
		e.ResponseUsage != "full" || e.SpawnedBy != "parent" || e.SpawnedWorkspaceDir != "/workspace" ||
		e.SpawnDepth == nil || *e.SpawnDepth != 2 || e.SubagentRole != "reviewer" || e.SubagentControlScope != "children" {
		t.Fatalf("patched entry = %#v", e)
	}
	if stored := mgr.Get(got.Key); stored == nil || stored.Label != e.Label || stored.Model != e.Model {
		t.Fatalf("manager state = %#v", stored)
	}
}

func TestSessionsPatchValidationMalformedAndIdempotentPatch(t *testing.T) {
	mgr := runtimesession.NewManager()
	methods := Methods(Deps{Sessions: mgr})
	for _, key := range []string{"", "   ", "\t\n"} {
		resp := rpctest.Call(methods, "sessions.patch", map[string]any{"key": key})
		rpctest.MustErr(t, resp)
		if resp.Error.Code != protocol.ErrMissingParam {
			t.Errorf("key %q code = %q", key, resp.Error.Code)
		}
	}
	malformed := methods["sessions.patch"](context.Background(), &protocol.RequestFrame{
		ID: "bad", Method: "sessions.patch", Params: json.RawMessage(`{"key":`),
	})
	rpctest.MustErr(t, malformed)
	if malformed.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("malformed patch code = %q", malformed.Error.Code)
	}

	first := decodeSessionPayload[map[string]json.RawMessage](t, rpctest.Call(methods, "sessions.patch", map[string]any{
		"key": "same", "label": "stable",
	}))
	var firstEntry runtimesession.Session
	_ = json.Unmarshal(first["entry"], &firstEntry)
	second := decodeSessionPayload[map[string]json.RawMessage](t, rpctest.Call(methods, "sessions.patch", map[string]any{
		"key": "same", "label": "stable",
	}))
	var secondEntry runtimesession.Session
	_ = json.Unmarshal(second["entry"], &secondEntry)
	if secondEntry.Label != "stable" || secondEntry.UpdatedAt != firstEntry.UpdatedAt {
		t.Fatalf("idempotent patch changed entry: first=%#v second=%#v", firstEntry, secondEntry)
	}
}

func TestSessionsPatchConcurrentIndependentFields(t *testing.T) {
	mgr := runtimesession.NewManager()
	methods := Methods(Deps{Sessions: mgr})
	const workers = 40
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			params := map[string]any{"key": "concurrent"}
			if i%2 == 0 {
				params["label"] = "label"
			} else {
				params["model"] = "model"
			}
			resp := rpctest.Call(methods, "sessions.patch", params)
			if resp == nil || resp.Error != nil {
				t.Errorf("patch %d response = %#v", i, resp)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	got := mgr.Get("concurrent")
	if got == nil || got.Label != "label" || got.Model != "model" {
		t.Fatalf("concurrent patch lost fields: %#v", got)
	}
}

func TestSessionsResetMissingExistingAndReasonNormalization(t *testing.T) {
	mgr := runtimesession.NewManager()
	methods := Methods(Deps{Sessions: mgr})
	resp := rpctest.Call(methods, "sessions.reset", map[string]any{"key": "missing"})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrNotFound || !strings.Contains(string(resp.Error.Details), "missing") {
		t.Fatalf("missing reset error = %#v", resp.Error)
	}

	start := int64(100)
	end := int64(200)
	tokens := int64(50)
	mgr.Set(&runtimesession.Session{
		Key: "client:main", Kind: runtimesession.KindDirect, Status: runtimesession.StatusDone,
		StartedAt: &start, EndedAt: &end, RuntimeMs: &end, InputTokens: &tokens, OutputTokens: &tokens, TotalTokens: &tokens,
		AbortedLastRun: true, Label: "keep label", Model: "keep model",
	})
	type payload struct {
		OK    bool                    `json:"ok"`
		Key   string                  `json:"key"`
		Entry *runtimesession.Session `json:"entry"`
	}
	got := decodeSessionPayload[payload](t, rpctest.Call(methods, "sessions.reset", map[string]any{
		"key": " client:main ", "reason": "new",
	}))
	if !got.OK || got.Key != "client:main" || got.Entry == nil {
		t.Fatalf("reset payload = %#v", got)
	}
	e := got.Entry
	if e.Status != "" || e.StartedAt != nil || e.EndedAt != nil || e.RuntimeMs != nil || e.AbortedLastRun ||
		e.InputTokens != nil || e.OutputTokens != nil || e.TotalTokens != nil {
		t.Fatalf("reset runtime state = %#v", e)
	}
	if e.Label != "keep label" || e.Model != "keep model" {
		t.Fatalf("reset erased configuration: %#v", e)
	}
}

func TestSessionsOverflowCheckBoundaryAndMalformedValues(t *testing.T) {
	methods := Methods(Deps{})
	cases := []struct {
		name       string
		current    int64
		max        int64
		overflow   bool
		usage      float64
		pruneRatio float64
	}{
		{"disabled zero", 100, 0, false, 0, 0},
		{"disabled negative max", 100, -1, false, 0, 0},
		{"empty", 0, 100, false, 0, 0},
		{"negative current", -10, 100, false, 0, 0},
		{"below threshold", 70, 100, false, 0.7, 0},
		{"exact threshold", 90, 100, false, 0.9, (0.9 - 0.7) / 0.9},
		{"above threshold", 91, 100, true, 0.91, (0.91 - 0.7) / 0.91},
		{"at capacity", 100, 100, true, 1, 0.3},
		{"far over clamps", 300, 100, true, 3, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeSessionPayload[map[string]any](t, rpctest.Call(methods, "sessions.overflow_check", map[string]any{
				"sessionKey": "s", "currentTokens": tc.current, "maxTokens": tc.max,
			}))
			if got["isOverflow"] != tc.overflow || got["usage"] != tc.usage {
				t.Fatalf("overflow result = %#v", got)
			}
			if tc.max <= 0 {
				if _, ok := got["emergencyPruneRatio"]; ok {
					t.Fatalf("disabled max emitted prune ratio: %#v", got)
				}
				return
			}
			pruneRatio, _ := got["emergencyPruneRatio"].(float64)
			if math.Abs(pruneRatio-tc.pruneRatio) > 1e-12 {
				t.Fatalf("prune ratio = %#v, want %v", got["emergencyPruneRatio"], tc.pruneRatio)
			}
		})
	}
}

type transcriptRecorder struct {
	mu      sync.Mutex
	deleted []string
	err     error
}

func (r *transcriptRecorder) Delete(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, key)
	return r.err
}

func (r *transcriptRecorder) keys() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.deleted...)
}

func TestSessionsCRUDListGetValidationAndCopies(t *testing.T) {
	mgr := runtimesession.NewManager()
	mgr.Create("b", runtimesession.KindGroup)
	mgr.Create("a", runtimesession.KindDirect)
	methods := CRUDMethods(Deps{Sessions: mgr})

	listed := decodeSessionPayload[[]runtimesession.Session](t, rpctest.Call(methods, "sessions.list", nil))
	if len(listed) != 2 {
		t.Fatalf("list = %#v", listed)
	}
	keys := []string{listed[0].Key, listed[1].Key}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"a", "b"}) {
		t.Fatalf("list keys = %#v", keys)
	}

	got := decodeSessionPayload[runtimesession.Session](t, rpctest.Call(methods, "sessions.get", map[string]any{"key": " a "}))
	if got.Key != "a" || got.Kind != runtimesession.KindDirect {
		t.Fatalf("get = %#v", got)
	}
	for _, params := range []map[string]any{{}, {"key": "   "}, {"key": "missing"}} {
		resp := rpctest.Call(methods, "sessions.get", params)
		rpctest.MustErr(t, resp)
		if params["key"] == "missing" && resp.Error.Code != protocol.ErrNotFound {
			t.Errorf("missing get code = %q", resp.Error.Code)
		}
	}
	malformed := methods["sessions.get"](context.Background(), &protocol.RequestFrame{ID: "bad", Params: json.RawMessage(`{`)})
	rpctest.MustErr(t, malformed)
}

func TestSessionsDeleteRunningForceAndTranscriptFallbacks(t *testing.T) {
	mgr := runtimesession.NewManager()
	mgr.Set(&runtimesession.Session{Key: "running", Kind: runtimesession.KindDirect, Status: runtimesession.StatusRunning})
	recorder := &transcriptRecorder{}
	methods := CRUDMethods(Deps{
		Sessions: mgr,
		Transcripts: func() (TranscriptDeleter, error) {
			return recorder, nil
		},
	})
	resp := rpctest.Call(methods, "sessions.delete", map[string]any{"key": "running"})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrConflict || mgr.Get("running") == nil || len(recorder.keys()) != 0 {
		t.Fatalf("running delete = %#v session=%#v transcript=%#v", resp.Error, mgr.Get("running"), recorder.keys())
	}

	deleted := decodeSessionPayload[map[string]bool](t, rpctest.Call(methods, "sessions.delete", map[string]any{
		"key": "running", "force": true,
	}))
	if !deleted["deleted"] || mgr.Get("running") != nil || !reflect.DeepEqual(recorder.keys(), []string{"running"}) {
		t.Fatalf("forced delete = %#v session=%#v transcript=%#v", deleted, mgr.Get("running"), recorder.keys())
	}

	// Missing in-memory sessions still delete transcripts to prevent a stale
	// JSONL file from resurrecting them on restart.
	deleted = decodeSessionPayload[map[string]bool](t, rpctest.Call(methods, "sessions.delete", map[string]any{"key": "zombie"}))
	if deleted["deleted"] || !reflect.DeepEqual(recorder.keys(), []string{"running", "zombie"}) {
		t.Fatalf("zombie cleanup = %#v transcript=%#v", deleted, recorder.keys())
	}

	resolverErr := errors.New("store unavailable")
	methods = CRUDMethods(Deps{
		Sessions: mgr,
		Transcripts: func() (TranscriptDeleter, error) {
			return nil, resolverErr
		},
	})
	deleted = decodeSessionPayload[map[string]bool](t, rpctest.Call(methods, "sessions.delete", map[string]any{"key": "missing"}))
	if deleted["deleted"] {
		t.Fatalf("resolver failure changed deletion result: %#v", deleted)
	}

	recorder.err = errors.New("disk read-only")
	methods = CRUDMethods(Deps{Sessions: mgr, Transcripts: func() (TranscriptDeleter, error) { return recorder, nil }})
	deleted = decodeSessionPayload[map[string]bool](t, rpctest.Call(methods, "sessions.delete", map[string]any{"key": "partial-io"}))
	if deleted["deleted"] || !strings.Contains(recorder.err.Error(), "read-only") {
		t.Fatalf("transcript deletion failure response = %#v", deleted)
	}
}

func newJobTracker() *agent.JobTracker {
	return agent.NewJobTracker(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestAgentWaitValidationUnavailableCachedAndTimeout(t *testing.T) {
	h := agentWait(ExecDeps{})
	for _, params := range []map[string]any{{}, {"runId": "run"}} {
		resp := rpctest.Call(map[string]rpcutil.HandlerFunc{"wait": h}, "wait", params)
		rpctest.MustErr(t, resp)
		want := protocol.ErrMissingParam
		if params["runId"] == "run" {
			want = protocol.ErrUnavailable
		}
		if resp.Error.Code != want || !strings.Contains(string(resp.Error.Details), "agent.wait") {
			t.Errorf("params %#v error = %#v", params, resp.Error)
		}
	}

	tracker := newJobTracker()
	now := time.Now().UnixMilli()
	tracker.OnLifecycleEvent(agent.LifecycleEvent{RunID: "cached", Phase: "start", Ts: now})
	tracker.OnLifecycleEvent(agent.LifecycleEvent{RunID: "cached", Phase: "end", Ts: now + 5})
	h = agentWait(ExecDeps{JobTracker: tracker})
	cached := decodeSessionPayload[agent.RunSnapshot](t, rpctest.Call(
		map[string]rpcutil.HandlerFunc{"wait": h},
		"wait", map[string]any{"runId": "cached", "timeoutMs": 1},
	))
	if cached.RunID != "cached" || cached.Status != agent.RunStatusOK {
		t.Fatalf("cached wait = %#v", cached)
	}

	start := time.Now()
	timedOut := decodeSessionPayload[map[string]any](t, rpctest.Call(
		map[string]rpcutil.HandlerFunc{"wait": h},
		"wait", map[string]any{"runId": "missing", "timeoutMs": 5},
	))
	if timedOut["status"] != "timeout" || !strings.Contains(fmt.Sprint(timedOut["message"]), "did not complete") || time.Since(start) > time.Second {
		t.Fatalf("timeout wait = %#v duration=%s", timedOut, time.Since(start))
	}
}

func TestAgentWaitAsyncCompletionCancellationAndIgnoreCached(t *testing.T) {
	tracker := newJobTracker()
	h := agentWait(ExecDeps{JobTracker: tracker})
	methods := map[string]rpcutil.HandlerFunc{"wait": h}

	done := make(chan *protocol.ResponseFrame, 1)
	go func() {
		done <- rpctest.Call(methods, "wait", map[string]any{"runId": "async", "timeoutMs": 2_000})
	}()
	time.Sleep(10 * time.Millisecond)
	now := time.Now().UnixMilli()
	tracker.OnLifecycleEvent(agent.LifecycleEvent{RunID: "async", Phase: "start", Ts: now})
	tracker.OnLifecycleEvent(agent.LifecycleEvent{RunID: "async", Phase: "end", Ts: now + 2})
	select {
	case resp := <-done:
		got := decodeSessionPayload[agent.RunSnapshot](t, resp)
		if got.RunID != "async" || got.Status != agent.RunStatusOK {
			t.Fatalf("async wait = %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("async wait did not complete")
	}

	tracker.OnLifecycleEvent(agent.LifecycleEvent{RunID: "stale", Phase: "start", Ts: now})
	tracker.OnLifecycleEvent(agent.LifecycleEvent{RunID: "stale", Phase: "end", Ts: now + 1})
	ignored := decodeSessionPayload[map[string]any](t, rpctest.Call(methods, "wait", map[string]any{
		"runId": "stale", "timeoutMs": 5, "ignoreCached": true,
	}))
	if ignored["status"] != "timeout" {
		t.Fatalf("ignoreCached wait = %#v", ignored)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(map[string]any{"runId": "cancel", "timeoutMs": 60_000})
	start := time.Now()
	resp := h(ctx, &protocol.RequestFrame{ID: "cancel", Method: "agent.wait", Params: raw})
	cancelled := decodeSessionPayload[map[string]any](t, resp)
	if cancelled["status"] != "timeout" || time.Since(start) > time.Second {
		t.Fatalf("cancelled wait = %#v duration=%s", cancelled, time.Since(start))
	}
}

func TestMinMaxHelpersHandleOrderingEqualityAndNaNLikeBoundaries(t *testing.T) {
	if minf(1, 2) != 1 || minf(2, 1) != 1 || minf(1, 1) != 1 || minf(-2, -1) != -2 {
		t.Fatal("minf ordering incorrect")
	}
	if maxf(1, 2) != 2 || maxf(2, 1) != 2 || maxf(1, 1) != 1 || maxf(-2, -1) != -1 {
		t.Fatal("maxf ordering incorrect")
	}
	// Nil subscriptions are the normal embedded/CLI fallback.
	emitSessionLifecycle(nil, "session", "patch")
}
