package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const ciHealthMethod = "health.check"

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// testDispatcher creates a dispatcher with all built-in methods registered:
// catalog (via RegisterBuiltinMethods) + session CRUD + health/system.info
// + telegram status queries.
func testDispatcher() *Dispatcher {
	d := NewDispatcher(testLogger())
	sm := session.NewManager()
	RegisterBuiltinMethods(d)
	RegisterSessionCRUDMethods(d, SessionDeps{Sessions: sm})
	RegisterTelegramStatusMethods(d, TelegramStatusDeps{})
	RegisterHealthMethods(d, SystemHealthDeps{})
	return d
}

func dispatch(t *testing.T, d *Dispatcher, method string, params any) *protocol.ResponseFrame {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		b, _ := json.Marshal(params)
		raw = b
	}
	req := &protocol.RequestFrame{Type: "req", ID: "test-1", Method: method, Params: raw}
	return d.Dispatch(context.Background(), req)
}

func TestBuiltinMethodsRegistered(t *testing.T) {
	d := testDispatcher()
	methods := d.Methods()
	if len(methods) < 10 {
		t.Errorf("got %d: %v, want at least 10 built-in methods", len(methods), methods)
	}
	expected := []string{
		"health.check", "sessions.get", "sessions.list", "sessions.delete",
		"telegram.list", "telegram.get", "telegram.status", "telegram.health",
		"system.info",
		// Tools catalog (static core).
		"tools.catalog",
	}
	set := make(map[string]struct{})
	for _, m := range methods {
		set[m] = struct{}{}
	}
	for _, e := range expected {
		if _, ok := set[e]; !ok {
			t.Errorf("expected method %q to be registered", e)
		}
	}
}

func TestHealthCheck(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, ciHealthMethod, nil)
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if payload["status"] != "ok" {
		t.Errorf("got %v, want status=ok", payload["status"])
	}
	if payload["runtime"] != "go" {
		t.Errorf("got %v, want runtime=go", payload["runtime"])
	}
}
func TestRPCSmokeFrequentMethods(t *testing.T) {
	d := testDispatcher()
	tests := []struct {
		name         string
		method       string
		params       any
		expectOK     bool
		expectErr    string
		expectFields []string
	}{
		{
			name:         "health check",
			method:       ciHealthMethod,
			expectOK:     true,
			expectFields: []string{"status", "runtime", "sessions", "channels"},
		},
		{
			name:         "system info",
			method:       "system.info",
			expectOK:     true,
			expectFields: []string{"runtime", "version", "goVersion", "os", "arch", "numCPU"},
		},
		{
			name:     "sessions list",
			method:   "sessions.list",
			expectOK: true,
		},
		{
			name:     "telegram status",
			method:   "telegram.status",
			expectOK: true,
		},
		{
			name:         "telegram health",
			method:       "telegram.health",
			expectOK:     true,
			expectFields: []string{"channels"},
		},
		{
			name:      "unknown method",
			method:    "nonexistent.method",
			expectOK:  false,
			expectErr: protocol.ErrNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := dispatch(t, d, tc.method, tc.params)
			if tc.expectOK != resp.OK {
				t.Fatalf("got ok=%v (error=%+v), want ok=%v", resp.OK, resp.Error, tc.expectOK)
			}
			if tc.expectErr != "" {
				if resp.Error == nil || resp.Error.Code != tc.expectErr {
					t.Fatalf("got %+v, want error code %q", resp.Error, tc.expectErr)
				}
			}
			if len(tc.expectFields) == 0 {
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(resp.Payload, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			for _, field := range tc.expectFields {
				if _, ok := payload[field]; !ok {
					t.Errorf("missing %q in payload: %#v", field, payload)
				}
			}
		})
	}
}

func TestSessionsGet_NotFound(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "sessions.get", map[string]string{"key": "nonexistent"})
	if resp.OK {
		t.Error("expected error for nonexistent session")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("got %+v, want NOT_FOUND", resp.Error)
	}
}

func TestRegisterCoreBuiltins_DuplicateMethodValidation(t *testing.T) {
	d := NewDispatcher(testLogger())
	d.beginRegistryValidation()
	d.setRegistryModule("core.health")
	d.Register("health.check", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.MustResponseOK(req.ID, map[string]any{"ok": true})
	})
	d.setRegistryModule("core.session")
	d.Register("health.check", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.MustResponseOK(req.ID, map[string]any{"ok": true})
	})
	err := d.endRegistryValidation()
	if err == nil {
		t.Fatal("expected duplicate registration error")
	}
	if !strings.Contains(err.Error(), "duplicate rpc method") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionsDelete(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "test-1", Kind: session.KindDirect})

	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d)
	RegisterSessionCRUDMethods(d, SessionDeps{Sessions: sm})

	resp := dispatch(t, d, "sessions.delete", map[string]string{"key": "test-1"})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
	if sm.Get("test-1") != nil {
		t.Error("session should have been deleted")
	}
}

func TestSessionsDelete_RunningBlocked(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "run-1", Kind: session.KindDirect, Status: session.StatusRunning})

	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d)
	RegisterSessionCRUDMethods(d, SessionDeps{Sessions: sm})

	// Without force: should be rejected.
	resp := dispatch(t, d, "sessions.delete", map[string]any{"key": "run-1"})
	if resp.OK {
		t.Fatal("expected CONFLICT error for running session without force")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrConflict {
		t.Errorf("got %+v, want CONFLICT", resp.Error)
	}
	if sm.Get("run-1") == nil {
		t.Fatal("running session should NOT have been deleted")
	}

	// With force=true: should succeed.
	resp = dispatch(t, d, "sessions.delete", map[string]any{"key": "run-1", "force": true})
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok with force=true", resp.Error)
	}
	if sm.Get("run-1") != nil {
		t.Error("session should have been deleted with force")
	}
}

func TestSessionsList(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "s1", Kind: session.KindDirect})
	sm.Set(&session.Session{Key: "s2", Kind: session.KindGroup})

	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d)
	RegisterSessionCRUDMethods(d, SessionDeps{Sessions: sm})

	resp := dispatch(t, d, "sessions.list", nil)
	if !resp.OK {
		t.Fatalf("got error: %+v, want ok", resp.Error)
	}
}
