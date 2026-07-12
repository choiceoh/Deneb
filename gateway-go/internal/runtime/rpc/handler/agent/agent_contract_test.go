package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestExtendedMethodsExactDependencySurface(t *testing.T) {
	sessions := session.NewManager()
	base := ExtendedMethods(ExtendedDeps{Sessions: sessions})
	baseNames := make([]string, 0, len(base))
	for name := range base {
		baseNames = append(baseNames, name)
	}
	sort.Strings(baseNames)
	if want := []string{"agent.status", "sessions.create", "sessions.lifecycle"}; !reflect.DeepEqual(baseNames, want) {
		t.Fatalf("base methods = %#v, want %#v", baseNames, want)
	}

	processes := process.NewManager(nil)
	t.Cleanup(processes.Stop)
	withProcesses := ExtendedMethods(ExtendedDeps{Sessions: sessions, Processes: processes})
	for _, name := range []string{"process.exec", "process.kill", "process.get", "process.list"} {
		if withProcesses[name] == nil {
			t.Errorf("missing process method %q", name)
		}
	}
	for _, name := range []string{"cron.list", "cron.get", "cron.unregister"} {
		if withProcesses[name] != nil {
			t.Errorf("unexpected cron method %q", name)
		}
	}
}

func TestSessionsCreateTrimsValidatesKindAndReplaces(t *testing.T) {
	manager := session.NewManager()
	methods := ExtendedMethods(ExtendedDeps{Sessions: manager})
	for _, params := range []map[string]any{
		{},
		{"key": ""},
		{"key": "   "},
		{"key": "bad\x00key"},
		{"key": strings.Repeat("x", 513)},
	} {
		response := rpctest.Call(methods, "sessions.create", params)
		rpctest.MustErr(t, response)
	}
	response := rpctest.Call(methods, "sessions.create", map[string]any{"key": " client:test ", "kind": " direct "})
	rpctest.MustOK(t, response)
	created := manager.Get("client:test")
	if created == nil || created.Key != "client:test" || created.Kind != session.KindDirect {
		t.Fatalf("created = %+v", created)
	}
	if manager.Get(" client:test ") != nil {
		t.Fatal("untrimmed session key was stored")
	}

	response = rpctest.Call(methods, "sessions.create", map[string]any{"key": "client:unknown-kind", "kind": "not-a-kind"})
	rpctest.MustOK(t, response)
	if got := manager.Get("client:unknown-kind"); got == nil || got.Kind != session.KindDirect {
		t.Fatalf("unknown kind session = %+v", got)
	}
}

func TestSessionsCreateMalformedJSON(t *testing.T) {
	methods := ExtendedMethods(ExtendedDeps{Sessions: session.NewManager()})
	request := &protocol.RequestFrame{ID: "bad", Params: json.RawMessage(`{`)}
	response := methods["sessions.create"](context.Background(), request)
	if response.Error == nil || response.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("response = %+v", response)
	}
}

func TestSessionsLifecycleValidationAndTransitions(t *testing.T) {
	manager := session.NewManager()
	methods := ExtendedMethods(ExtendedDeps{Sessions: manager})
	for _, params := range []map[string]any{
		{},
		{"key": "k"},
		{"phase": "start"},
		{"key": " ", "phase": "start"},
		{"key": "k", "phase": " "},
		{"key": "bad\x00", "phase": "start"},
		{"key": "k", "phase": "bogus"},
	} {
		response := rpctest.Call(methods, "sessions.lifecycle", params)
		rpctest.MustErr(t, response)
	}

	startedAt := int64(100)
	response := rpctest.Call(methods, "sessions.lifecycle", map[string]any{
		"key": " lifecycle:key ", "phase": " start ", "ts": 90, "startedAt": startedAt,
	})
	rpctest.MustOK(t, response)
	running := manager.Get("lifecycle:key")
	if running == nil || running.Status != session.StatusRunning || running.StartedAt == nil || *running.StartedAt != 100 {
		t.Fatalf("running = %+v", running)
	}
	endedAt := int64(175)
	response = rpctest.Call(methods, "sessions.lifecycle", map[string]any{
		"key": "lifecycle:key", "phase": "end", "ts": 180, "endedAt": endedAt,
	})
	rpctest.MustOK(t, response)
	done := manager.Get("lifecycle:key")
	if done.Status != session.StatusDone || done.EndedAt == nil || *done.EndedAt != 175 || done.RuntimeMs == nil || *done.RuntimeMs != 75 {
		t.Fatalf("done = %+v", done)
	}

	response = rpctest.Call(methods, "sessions.lifecycle", map[string]any{
		"key": "failed:key", "phase": "error", "ts": 200, "stopReason": "failure",
	})
	rpctest.MustOK(t, response)
	if got := manager.Get("failed:key"); got == nil || got.Status != session.StatusFailed {
		t.Fatalf("failed = %+v", got)
	}
}

func TestAgentStatusCountsRunningAndOptionalProcesses(t *testing.T) {
	manager := session.NewManager()
	manager.Create("idle", session.KindDirect)
	manager.ApplyLifecycleEvent("running", session.LifecycleEvent{Phase: session.PhaseStart, Ts: 100})
	manager.ApplyLifecycleEvent("done", session.LifecycleEvent{Phase: session.PhaseEnd, Ts: 200})
	methods := ExtendedMethods(ExtendedDeps{Sessions: manager})
	response := rpctest.Call(methods, "agent.status", nil)
	rpctest.MustOK(t, response)
	result := rpctest.Result(t, response)
	if result["activeSessions"] != float64(1) || result["totalSessions"] != float64(3) {
		t.Fatalf("status = %#v", result)
	}
	channels, ok := result["channels"].([]any)
	if !ok || len(channels) != 0 {
		t.Fatalf("channels = %#v", result["channels"])
	}
	if _, exists := result["activeProcesses"]; exists {
		t.Fatalf("status has process count without dependency: %#v", result)
	}

	processes := process.NewManager(nil)
	t.Cleanup(processes.Stop)
	withProcesses := ExtendedMethods(ExtendedDeps{Sessions: manager, Processes: processes})
	result = rpctest.Result(t, rpctest.Call(withProcesses, "agent.status", nil))
	if result["activeProcesses"] != float64(0) {
		t.Fatalf("activeProcesses = %#v", result["activeProcesses"])
	}
}

func TestProcessHandlersValidationListAndMissing(t *testing.T) {
	manager := process.NewManager(nil)
	t.Cleanup(manager.Stop)
	methods := ExtendedMethods(ExtendedDeps{Sessions: session.NewManager(), Processes: manager})
	for _, tc := range []struct {
		method string
		params map[string]any
	}{
		{method: "process.exec", params: map[string]any{}},
		{method: "process.exec", params: map[string]any{"command": "   "}},
		{method: "process.get", params: map[string]any{}},
		{method: "process.get", params: map[string]any{"id": " "}},
		{method: "process.kill", params: map[string]any{}},
		{method: "process.kill", params: map[string]any{"id": "missing"}},
	} {
		response := rpctest.Call(methods, tc.method, tc.params)
		rpctest.MustErr(t, response)
	}
	response := rpctest.Call(methods, "process.list", nil)
	rpctest.MustOK(t, response)
	var listed []any
	if err := json.Unmarshal(response.Payload, &listed); err != nil || len(listed) != 0 {
		t.Fatalf("process list = %s err=%v", response.Payload, err)
	}
}

func TestSessionHandlersConcurrentCreateAndLifecycle(t *testing.T) {
	manager := session.NewManager()
	methods := ExtendedMethods(ExtendedDeps{Sessions: manager})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				key := "session-" + string(rune('a'+(i+j)%20))
				rpctest.Call(methods, "sessions.create", map[string]any{"key": key, "kind": "direct"})
				rpctest.Call(methods, "sessions.lifecycle", map[string]any{"key": key, "phase": "start", "ts": j + 1})
				if j%2 == 0 {
					rpctest.Call(methods, "sessions.lifecycle", map[string]any{"key": key, "phase": "end", "ts": j + 2})
				}
			}
		}(i)
	}
	wg.Wait()
	if manager.Count() == 0 {
		t.Fatal("concurrent handlers created no sessions")
	}
}
