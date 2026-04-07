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

const (
	ciHealthMethod = "health.check"
	ciValidFrame   = `{"type":"req","id":"1","method":"ping"}`
	ciInvalidFrame = `{"type":"unknown"}`
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testDeps() Deps {
	return Deps{
		Sessions: session.NewManager(),
	}
}

func testDispatcher() *Dispatcher {
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, testDeps())
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
	if len(methods) < 20 {
		t.Errorf("expected at least 20 built-in methods, got %d: %v", len(methods), methods)
	}
	expected := []string{
		"health.check", "sessions.get", "sessions.list", "sessions.delete",
		"telegram.list", "telegram.get", "telegram.status", "telegram.health",
		"system.info", "protocol.validate", "protocol.validate_params",
		"security.validate_session_key", "security.sanitize_html",
		"security.is_safe_url", "security.validate_error_code",
		"media.detect_mime",
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
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if payload["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", payload["status"])
	}
	if payload["runtime"] != "go" {
		t.Errorf("expected runtime=go, got %v", payload["runtime"])
	}
}

func TestSystemInfo(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "system.info", nil)
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if payload["runtime"] != "go" {
		t.Errorf("expected runtime=go, got %v", payload["runtime"])
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
				t.Fatalf("expected ok=%v, got ok=%v (error=%+v)", tc.expectOK, resp.OK, resp.Error)
			}
			if tc.expectErr != "" {
				if resp.Error == nil || resp.Error.Code != tc.expectErr {
					t.Fatalf("expected error code %q, got %+v", tc.expectErr, resp.Error)
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

func TestProtocolValidate_FrameContracts(t *testing.T) {
	d := testDispatcher()
	tests := []struct {
		name        string
		frame       string
		expectValid bool
	}{
		{
			name:        "valid request frame",
			frame:       ciValidFrame,
			expectValid: true,
		},
		{
			name:        "invalid frame type",
			frame:       ciInvalidFrame,
			expectValid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := dispatch(t, d, "protocol.validate", map[string]string{"frame": tc.frame})
			if !resp.OK {
				t.Fatalf("expected ok (validation result in payload), got error: %+v", resp.Error)
			}
			var payload map[string]any
			if err := json.Unmarshal(resp.Payload, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if payload["valid"] != tc.expectValid {
				t.Errorf("expected valid=%v, got %v", tc.expectValid, payload["valid"])
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
		t.Errorf("expected NOT_FOUND, got %+v", resp.Error)
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
	RegisterBuiltinMethods(d, Deps{Sessions: sm})

	resp := dispatch(t, d, "sessions.delete", map[string]string{"key": "test-1"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	if sm.Get("test-1") != nil {
		t.Error("session should have been deleted")
	}
}

func TestSessionsDelete_RunningBlocked(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "run-1", Kind: session.KindDirect, Status: session.StatusRunning})

	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: sm})

	// Without force: should be rejected.
	resp := dispatch(t, d, "sessions.delete", map[string]any{"key": "run-1"})
	if resp.OK {
		t.Fatal("expected CONFLICT error for running session without force")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrConflict {
		t.Errorf("expected CONFLICT, got %+v", resp.Error)
	}
	if sm.Get("run-1") == nil {
		t.Fatal("running session should NOT have been deleted")
	}

	// With force=true: should succeed.
	resp = dispatch(t, d, "sessions.delete", map[string]any{"key": "run-1", "force": true})
	if !resp.OK {
		t.Fatalf("expected ok with force=true, got error: %+v", resp.Error)
	}
	if sm.Get("run-1") != nil {
		t.Error("session should have been deleted with force")
	}
}

// ---------------------------------------------------------------------------
// Protocol validate_params RPC tests
// ---------------------------------------------------------------------------

func TestProtocolValidateParams_MissingMethod(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "protocol.validate_params", map[string]string{
		"params": `{"key":"value"}`,
	})
	if resp.OK {
		t.Error("expected error for missing method")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %+v", resp.Error)
	}
}

func TestProtocolValidateParams_MissingParams(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "protocol.validate_params", map[string]string{
		"method": ciHealthMethod,
	})
	if resp.OK {
		t.Error("expected error for missing params")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %+v", resp.Error)
	}
}

func TestSessionsList(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "s1", Kind: session.KindDirect})
	sm.Set(&session.Session{Key: "s2", Kind: session.KindGroup})

	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: sm})

	resp := dispatch(t, d, "sessions.list", nil)
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// Telegram method contract tests
// ---------------------------------------------------------------------------

func TestTelegramList(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "telegram.list", nil)
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}

func TestTelegramGet_MissingID(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "telegram.get", map[string]any{})
	if resp.OK {
		t.Error("expected error for missing id")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %+v", resp.Error)
	}
}

func TestTelegramGet_NotFound(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "telegram.get", map[string]string{"id": "nonexistent-chan"})
	if resp.OK {
		t.Error("expected error for nonexistent channel")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND, got %+v", resp.Error)
	}
}

func TestTelegramStatus(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "telegram.status", nil)
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// Security method contract tests
// ---------------------------------------------------------------------------

func TestSecurityValidateSessionKey_Valid(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "security.validate_session_key", map[string]string{"key": "valid-session-key"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if _, ok := payload["valid"]; !ok {
		t.Error("expected valid field in response")
	}
}

func TestSecurityValidateSessionKey_EmptyKey(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "security.validate_session_key", map[string]string{"key": ""})
	if !resp.OK {
		t.Fatalf("expected ok (valid=false), got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if payload["valid"] != false {
		t.Errorf("expected valid=false for empty key, got %v", payload["valid"])
	}
}

func TestSecuritySanitizeHTML_Valid(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "security.sanitize_html", map[string]string{
		"input": "<b>hello</b><script>alert(1)</script>",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if _, ok := payload["output"]; !ok {
		t.Error("expected output field in response")
	}
}

func TestSecurityIsURL_ReturnsOK(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "security.is_safe_url", map[string]string{
		"url": "https://example.com/path",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if _, ok := payload["safe"]; !ok {
		t.Error("expected safe field in response")
	}
}

func TestSecurityValidateErrorCode_KnownCode(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "security.validate_error_code", map[string]string{"code": "NOT_FOUND"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if payload["valid"] != true {
		t.Errorf("expected valid=true for NOT_FOUND code, got %v", payload["valid"])
	}
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// Markdown method contract tests
// ---------------------------------------------------------------------------

func TestMarkdownDetectFences_Valid(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "markdown.detect_fences", map[string]string{
		"text": "```go\nfmt.Println(\"hello\")\n```",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if _, ok := payload["fences"]; !ok {
		t.Error("expected fences field in response")
	}
}
