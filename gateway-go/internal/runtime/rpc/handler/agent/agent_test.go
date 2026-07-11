package agent

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

func TestExtendedMethodsRegistersDependencyAwareSurface(t *testing.T) {
	base := ExtendedMethods(ExtendedDeps{})
	for _, name := range []string{"agent.status", "sessions.create", "sessions.lifecycle"} {
		if base[name] == nil {
			t.Errorf("missing always-on method %q", name)
		}
	}
	for _, name := range []string{"process.exec", "process.kill", "cron.list", "cron.get"} {
		if base[name] != nil {
			t.Errorf("method %q registered without dependency", name)
		}
	}
}

func TestSessionsCreateValidatesAndCreates(t *testing.T) {
	mgr := session.NewManager()
	methods := ExtendedMethods(ExtendedDeps{Sessions: mgr})

	rpctest.MustErr(t, rpctest.Call(methods, "sessions.create", map[string]any{}))
	rpctest.MustErr(t, rpctest.Call(methods, "sessions.create", map[string]any{"key": "bad\x00key"}))

	resp := rpctest.Call(methods, "sessions.create", map[string]any{
		"key":  "client:health-test",
		"kind": "direct",
	})
	rpctest.MustOK(t, resp)
	if got := mgr.Get("client:health-test"); got == nil {
		t.Fatal("session was not created")
	}
}

func TestAgentStatusCountsSessions(t *testing.T) {
	mgr := session.NewManager()
	mgr.Create("client:one", session.KindDirect)
	methods := ExtendedMethods(ExtendedDeps{Sessions: mgr})

	resp := rpctest.Call(methods, "agent.status", nil)
	rpctest.MustOK(t, resp)
	result := rpctest.Result(t, resp)
	if result["totalSessions"] != float64(1) {
		t.Fatalf("totalSessions = %v", result["totalSessions"])
	}
}
