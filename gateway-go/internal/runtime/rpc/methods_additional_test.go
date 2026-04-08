package rpc

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)



func TestSessionsGetMissingKeyAndSuccess(t *testing.T) {
	sm := session.NewManager()
	sm.Set(&session.Session{Key: "abc", Kind: session.KindDirect})
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d)
	RegisterSessionCRUDMethods(d, SessionDeps{Sessions: sm})

	resp := dispatch(t, d, "sessions.get", map[string]any{})
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("got %+v, want missing param error", resp)
	}

	resp = dispatch(t, d, "sessions.get", map[string]any{"key": "abc"})
	if !resp.OK {
		t.Fatalf("got error %+v, want success", resp.Error)
	}
}

func TestTelegramGetMissingAndNotFound(t *testing.T) {
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d)
	RegisterTelegramStatusMethods(d, TelegramStatusDeps{})

	missing := dispatch(t, d, "telegram.get", map[string]any{})
	if missing.OK || missing.Error == nil || missing.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("got %+v, want missing param error", missing)
	}

	notFound := dispatch(t, d, "telegram.get", map[string]any{"id": "nope"})
	if notFound.OK || notFound.Error == nil || notFound.Error.Code != protocol.ErrNotFound {
		t.Fatalf("got %+v, want not found error", notFound)
	}

	// Without TelegramPlugin set, "telegram" should also be not found.
	notFound = dispatch(t, d, "telegram.get", map[string]any{"id": "telegram"})
	if notFound.OK || notFound.Error == nil || notFound.Error.Code != protocol.ErrNotFound {
		t.Fatalf("got %+v, want not found for telegram without plugin", notFound)
	}
}

