package session

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
)

var callMethod = rpctest.Call

// ---------------------------------------------------------------------------
// Methods (session management)
// ---------------------------------------------------------------------------

func TestSessionsPatch_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.patch", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSessionsReset_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.reset", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSessionsPreview_emptyKeys(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.preview", map[string]any{})
	if resp == nil {
		t.Fatal("expected response")
	}
	// sessions.preview with no keys returns an empty previews array, not an error.
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
}

func TestSessionsResolve_missingKey(t *testing.T) {
	m := Methods(Deps{})
	resp := callMethod(m, "sessions.resolve", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing key")
	}
}

// ---------------------------------------------------------------------------
// ExecMethods
// ---------------------------------------------------------------------------
