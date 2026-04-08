package autoresearch

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

var (
	callMethod    = rpctest.Call
	mustOK        = rpctest.MustOK
	mustErr       = rpctest.MustErr
	extractResult = rpctest.Result
)

// ── Methods registration ────────────────────────────────────────────────────

func TestMethods_registersAllHandlers(t *testing.T) {
	m := Methods(Deps{}) // nil Runner is fine -- Methods always returns the map
	expected := []string{
		"autoresearch.status",
		"autoresearch.start",
		"autoresearch.stop",
		"autoresearch.results",
		"autoresearch.config",
		"autoresearch.resume",
		"autoresearch.archive",
		"autoresearch.runs",
	}
	if len(m) != len(expected) {
		t.Errorf("expected %d handlers, got %d", len(expected), len(m))
	}
	for _, name := range expected {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

func TestMethods_nilRunner_stillRegistersAll(t *testing.T) {
	m := Methods(Deps{Runner: nil})
	if len(m) != 8 {
		t.Fatalf("expected 8 handlers with nil Runner, got %d", len(m))
	}
}

// ── requireRunner: all handlers return UNAVAILABLE when Runner is nil ────────

func TestNilRunner_statusUnavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.status", nil)
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
	}
}

func TestNilRunner_startUnavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.start", map[string]any{"workdir": "/tmp"})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
	}
}

func TestNilRunner_stopUnavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.stop", nil)
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
	}
}

func TestNilRunner_resultsUnavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.results", map[string]any{"workdir": "/tmp"})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
	}
}

func TestNilRunner_configUnavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.config", map[string]any{"workdir": "/tmp"})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
	}
}

func TestNilRunner_resumeUnavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.resume", map[string]any{"workdir": "/tmp"})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
	}
}

func TestNilRunner_archiveUnavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.archive", map[string]any{"workdir": "/tmp"})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
	}
}

func TestNilRunner_runsUnavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.runs", map[string]any{"workdir": "/tmp"})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
	}
}

// ── Table-driven: all 8 handlers are UNAVAILABLE with nil Runner ────────────

func TestNilRunner_allHandlersUnavailable(t *testing.T) {
	m := Methods(Deps{})
	cases := []struct {
		method string
		params map[string]any
	}{
		{"autoresearch.status", nil},
		{"autoresearch.start", map[string]any{"workdir": "/tmp"}},
		{"autoresearch.stop", nil},
		{"autoresearch.results", map[string]any{"workdir": "/tmp"}},
		{"autoresearch.config", map[string]any{"workdir": "/tmp"}},
		{"autoresearch.resume", map[string]any{"workdir": "/tmp"}},
		{"autoresearch.archive", map[string]any{"workdir": "/tmp"}},
		{"autoresearch.runs", map[string]any{"workdir": "/tmp"}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			resp := callMethod(m, tc.method, tc.params)
			mustErr(t, resp)
			if resp.Error.Code != protocol.ErrUnavailable {
				t.Errorf("expected %s, got %s: %s", protocol.ErrUnavailable, resp.Error.Code, resp.Error.Message)
			}
		})
	}
}

// ── requireRunner error message ─────────────────────────────────────────────

func TestNilRunner_errorMessage(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.status", nil)
	mustErr(t, resp)
	if resp.Error.Message != "autoresearch runner not initialized" {
		t.Errorf("unexpected error message: %q", resp.Error.Message)
	}
}

// ── start: missing workdir ──────────────────────────────────────────────────

// Note: With Bind[P], param decode fires before the runner nil check.
// When Runner is nil AND params are nil, INVALID_REQUEST is returned
// (param decode error) rather than UNAVAILABLE (runner check).

func TestStart_nilParams_invalidRequestBeforeRunnerCheck(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.start", nil)
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %s", resp.Error.Code)
	}
}

func TestConfig_nilParams_invalidRequestBeforeRunnerCheck(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.config", nil)
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %s", resp.Error.Code)
	}
}

func TestResume_nilParams_invalidRequestBeforeRunnerCheck(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.resume", nil)
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %s", resp.Error.Code)
	}
}

func TestArchive_nilParams_invalidRequestBeforeRunnerCheck(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.archive", nil)
	mustErr(t, resp)
	// Bind[P] decodes params before runner check, so nil params → INVALID_REQUEST.
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %s", resp.Error.Code)
	}
}

func TestRuns_nilParams_invalidRequestBeforeRunnerCheck(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.runs", nil)
	mustErr(t, resp)
	// Bind[P] decodes params before runner check, so nil params → INVALID_REQUEST.
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %s", resp.Error.Code)
	}
}

// ── response ID propagation ─────────────────────────────────────────────────

func TestNilRunner_responseIDPreserved(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.status", nil)
	mustErr(t, resp)
	if resp.ID != "t1" {
		t.Errorf("expected response ID %q, got %q", "t1", resp.ID)
	}
}

func TestNilRunner_responseNotOK(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.status", nil)
	mustErr(t, resp)
	if resp.OK {
		t.Error("expected OK=false for error response")
	}
}

// ── unknown method returns nil ──────────────────────────────────────────────

func TestUnknownMethod_returnsNil(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.nonexistent", nil)
	if resp != nil {
		t.Errorf("expected nil for unknown method, got %+v", resp)
	}
}

// ── status with nil params is valid (no DecodeParams needed) ────────────────

func TestStatus_nilParams_onlyRunnerCheckMatters(t *testing.T) {
	m := Methods(Deps{})
	// status does not call DecodeParams, so nil params is fine.
	// The only error should be from requireRunner.
	resp := callMethod(m, "autoresearch.status", nil)
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %s", resp.Error.Code)
	}
}

// ── stop with nil params is valid (no DecodeParams needed) ──────────────────

func TestStop_nilParams_onlyRunnerCheckMatters(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.stop", nil)
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %s", resp.Error.Code)
	}
}

// ── results with empty params ───────────────────────────────────────────────

func TestResults_emptyParams_unavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.results", map[string]any{})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %s", resp.Error.Code)
	}
}

// ── workdir-requiring handlers: empty workdir string ────────────────────────

func TestStart_emptyWorkdir_unavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.start", map[string]any{"workdir": ""})
	mustErr(t, resp)
	// Still UNAVAILABLE because requireRunner fires first.
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %s", resp.Error.Code)
	}
}

func TestConfig_emptyWorkdir_unavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.config", map[string]any{"workdir": ""})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %s", resp.Error.Code)
	}
}

func TestResume_emptyWorkdir_unavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.resume", map[string]any{"workdir": ""})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %s", resp.Error.Code)
	}
}

func TestArchive_emptyWorkdir_unavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.archive", map[string]any{"workdir": ""})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %s", resp.Error.Code)
	}
}

func TestRuns_emptyWorkdir_unavailable(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "autoresearch.runs", map[string]any{"workdir": ""})
	mustErr(t, resp)
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %s", resp.Error.Code)
	}
}
