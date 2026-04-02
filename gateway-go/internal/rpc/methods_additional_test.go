package rpc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestUnmarshalParamsErrors(t *testing.T) {
	var dst map[string]any
	if err := unmarshalParams(nil, &dst); err == nil {
		t.Fatal("expected error for missing params")
	}
	if err := unmarshalParams(json.RawMessage("{"), &dst); err == nil {
		t.Fatal("expected JSON unmarshal error")
	}
}

func TestTruncateForError_LongInput(t *testing.T) {
	short := "short"
	if got := truncateForError(short); got != short {
		t.Fatalf("expected unchanged short string, got %q", got)
	}

	long := strings.Repeat("k", maxKeyInErrorMsg+10)
	got := truncateForError(long)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
	if len(got) != maxKeyInErrorMsg+3 {
		t.Fatalf("expected length %d, got %d", maxKeyInErrorMsg+3, len(got))
	}
}

func TestSessionsGetMissingKeyAndSuccess(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "abc", Kind: session.KindDirect})
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: sm})

	resp := dispatch(t, d, "sessions.get", map[string]any{})
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("expected missing param error, got %+v", resp)
	}

	resp = dispatch(t, d, "sessions.get", map[string]any{"key": "abc"})
	if !resp.OK {
		t.Fatalf("expected success, got error %+v", resp.Error)
	}
}

func TestTelegramGetMissingAndNotFound(t *testing.T) {
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: session.NewManager()})

	missing := dispatch(t, d, "telegram.get", map[string]any{})
	if missing.OK || missing.Error == nil || missing.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("expected missing param error, got %+v", missing)
	}

	notFound := dispatch(t, d, "telegram.get", map[string]any{"id": "nope"})
	if notFound.OK || notFound.Error == nil || notFound.Error.Code != protocol.ErrNotFound {
		t.Fatalf("expected not found error, got %+v", notFound)
	}

	// Without TelegramPlugin set, "telegram" should also be not found.
	notFound = dispatch(t, d, "telegram.get", map[string]any{"id": "telegram"})
	if notFound.OK || notFound.Error == nil || notFound.Error.Code != protocol.ErrNotFound {
		t.Fatalf("expected not found for telegram without plugin, got %+v", notFound)
	}
}

func TestSystemInfoUnknownVersion(t *testing.T) {
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: session.NewManager()})
	resp := dispatch(t, d, "system.info", nil)
	if !resp.OK {
		t.Fatalf("expected success, got %+v", resp.Error)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["version"] != "unknown" {
		t.Fatalf("expected unknown version fallback, got %v", payload["version"])
	}
}
