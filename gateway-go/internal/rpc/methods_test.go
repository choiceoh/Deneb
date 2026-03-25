package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func testDeps() Deps {
	return Deps{
		Sessions: session.NewManager(),
		Channels: channel.NewRegistry(),
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
	if len(methods) < 30 {
		t.Errorf("expected at least 30 built-in methods, got %d: %v", len(methods), methods)
	}
	expected := []string{
		"health.check", "sessions.get", "sessions.list", "sessions.delete",
		"channels.list", "channels.get", "channels.status", "channels.health",
		"system.info", "protocol.validate", "protocol.validate_params",
		"security.validate_session_key", "security.sanitize_html",
		"security.is_safe_url", "security.validate_error_code",
		"media.detect_mime",
		// Compaction sweep lifecycle.
		"compaction.evaluate",
		"compaction.sweep.new", "compaction.sweep.start",
		"compaction.sweep.step", "compaction.sweep.drop",
		// Context engine lifecycle.
		"context.assembly.new", "context.assembly.start", "context.assembly.step",
		"context.expand.new", "context.expand.start", "context.expand.step",
		"context.engine.drop",
		// Vega FFI (Phase 0 scaffolding).
		"vega.ffi.execute", "vega.ffi.search",
		// ML FFI (Phase 0 scaffolding).
		"ml.embed", "ml.rerank",
		// Tools catalog (static core).
		"tools.catalog",
	}
	set := make(map[string]bool)
	for _, m := range methods {
		set[m] = true
	}
	for _, e := range expected {
		if !set[e] {
			t.Errorf("expected method %q to be registered", e)
		}
	}
}

func TestHealthCheck(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "health.check", nil)
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

func TestProtocolValidate_Valid(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "protocol.validate", map[string]string{
		"frame": `{"type":"req","id":"1","method":"ping"}`,
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if payload["valid"] != true {
		t.Errorf("expected valid=true, got %v", payload["valid"])
	}
}

func TestProtocolValidate_Invalid(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "protocol.validate", map[string]string{
		"frame": `{"type":"unknown"}`,
	})
	if !resp.OK {
		t.Fatalf("expected ok (with valid=false), got error: %+v", resp.Error)
	}
	var payload map[string]any
	json.Unmarshal(resp.Payload, &payload)
	if payload["valid"] != false {
		t.Errorf("expected valid=false, got %v", payload["valid"])
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

func TestSessionsDelete(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "test-1", Kind: session.KindDirect})

	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: sm, Channels: channel.NewRegistry()})

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
	RegisterBuiltinMethods(d, Deps{Sessions: sm, Channels: channel.NewRegistry()})

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
// Vega FFI RPC tests
// ---------------------------------------------------------------------------

func TestVegaFFIExecute(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "vega.ffi.execute", map[string]string{"cmd": "test"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}

func TestVegaFFISearch(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "vega.ffi.search", map[string]string{"query": "test"})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}

func TestVegaFFIExecute_MissingParams(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "vega.ffi.execute", nil)
	if resp.OK {
		t.Error("expected error for missing params")
	}
}

func TestVegaFFISearch_MissingParams(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "vega.ffi.search", nil)
	if resp.OK {
		t.Error("expected error for missing params")
	}
}

// ---------------------------------------------------------------------------
// ML FFI RPC tests
// ---------------------------------------------------------------------------

func TestMLEmbed(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "ml.embed", map[string]any{"texts": []string{"hello"}})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}

func TestMLRerank(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "ml.rerank", map[string]any{"query": "test", "docs": []string{"a", "b"}})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}

func TestMLEmbed_MissingParams(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "ml.embed", nil)
	if resp.OK {
		t.Error("expected error for missing params")
	}
}

func TestMLRerank_MissingParams(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "ml.rerank", nil)
	if resp.OK {
		t.Error("expected error for missing params")
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
		"method": "health.check",
	})
	if resp.OK {
		t.Error("expected error for missing params")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %+v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// Context engine RPC tests
// ---------------------------------------------------------------------------

func TestContextAssemblyNew_ReturnsHandle(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "context.assembly.new", map[string]any{
		"conversation_id":  1,
		"token_budget":     4096,
		"fresh_tail_count": 10,
	})
	// noffi returns an error (context engine not available), which is expected.
	if !ffi.Available && resp.OK {
		t.Error("expected error without FFI")
	}
	if ffi.Available && !resp.OK {
		t.Fatalf("expected ok with FFI, got error: %+v", resp.Error)
	}
}

func TestContextAssemblyStart_MissingHandle(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "context.assembly.start", map[string]any{})
	if resp.OK {
		t.Error("expected error for missing handle")
	}
}

func TestContextExpandNew_MissingSummaryID(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "context.expand.new", map[string]any{
		"max_depth": 3,
		"token_cap": 1024,
	})
	if resp.OK {
		t.Error("expected error for missing summary_id")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM, got %+v", resp.Error)
	}
}

func TestContextExpandStart_MissingHandle(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "context.expand.start", map[string]any{})
	if resp.OK {
		t.Error("expected error for missing handle")
	}
}

func TestContextEngineDrop_MissingHandle(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "context.engine.drop", map[string]any{})
	if resp.OK {
		t.Error("expected error for missing handle")
	}
}

// ---------------------------------------------------------------------------
// Compaction sweep RPC tests
// ---------------------------------------------------------------------------

func TestCompactionEvaluate(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "compaction.evaluate", map[string]any{
		"stored_tokens": 5000,
		"live_tokens":   3000,
		"token_budget":  8000,
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}

func TestCompactionSweepNew(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "compaction.sweep.new", map[string]any{
		"conversation_id": 1,
		"token_budget":    8000,
	})
	// noffi returns an error (sweep not available), which is expected.
	if !ffi.Available && resp.OK {
		t.Error("expected error without FFI")
	}
	if ffi.Available && !resp.OK {
		t.Fatalf("expected ok with FFI, got error: %+v", resp.Error)
	}
}

func TestCompactionSweepStart_MissingHandle(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "compaction.sweep.start", map[string]any{})
	if resp.OK {
		t.Error("expected error for missing handle")
	}
}

func TestCompactionSweepDrop_MissingHandle(t *testing.T) {
	d := testDispatcher()
	resp := dispatch(t, d, "compaction.sweep.drop", map[string]any{})
	if resp.OK {
		t.Error("expected error for missing handle")
	}
}

func TestSessionsList(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "s1", Kind: session.KindDirect})
	sm.Set(&session.Session{Key: "s2", Kind: session.KindGroup})

	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: sm, Channels: channel.NewRegistry()})

	resp := dispatch(t, d, "sessions.list", nil)
	if !resp.OK {
		t.Fatalf("expected ok, got error: %+v", resp.Error)
	}
}
