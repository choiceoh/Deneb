package autoresearch

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

var (
	callMethod = rpctest.Call
	mustErr    = rpctest.MustErr
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
