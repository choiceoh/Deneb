package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type stubChannel struct{}

func (stubChannel) ID() string         { return "stub" }
func (stubChannel) Meta() channel.Meta { return channel.Meta{ID: "stub", Label: "Stub"} }
func (stubChannel) Capabilities() channel.Capabilities {
	return channel.Capabilities{ChatTypes: []string{"dm"}}
}
func (stubChannel) Start(context.Context) error { return nil }
func (stubChannel) Stop(context.Context) error  { return nil }
func (stubChannel) Status() channel.Status      { return channel.Status{Connected: true} }

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
	RegisterBuiltinMethods(d, Deps{Sessions: sm, Channels: channel.NewRegistry()})

	resp := dispatch(t, d, "sessions.get", map[string]any{})
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("expected missing param error, got %+v", resp)
	}

	resp = dispatch(t, d, "sessions.get", map[string]any{"key": "abc"})
	if !resp.OK {
		t.Fatalf("expected success, got error %+v", resp.Error)
	}
}

func TestChannelsGetMissingAndSuccess(t *testing.T) {
	reg := channel.NewRegistry()
	if err := reg.Register(stubChannel{}); err != nil {
		t.Fatalf("register stub channel: %v", err)
	}
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: session.NewManager(), Channels: reg})

	missing := dispatch(t, d, "channels.get", map[string]any{})
	if missing.OK || missing.Error == nil || missing.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("expected missing param error, got %+v", missing)
	}

	notFound := dispatch(t, d, "channels.get", map[string]any{"id": "nope"})
	if notFound.OK || notFound.Error == nil || notFound.Error.Code != protocol.ErrNotFound {
		t.Fatalf("expected not found error, got %+v", notFound)
	}

	ok := dispatch(t, d, "channels.get", map[string]any{"id": "stub"})
	if !ok.OK {
		t.Fatalf("expected success, got error %+v", ok.Error)
	}
	var payload map[string]any
	if err := json.Unmarshal(ok.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["id"] != "stub" {
		t.Fatalf("expected id=stub, got %v", payload["id"])
	}
}

func TestSystemInfoUnknownVersion(t *testing.T) {
	d := NewDispatcher(testLogger())
	RegisterBuiltinMethods(d, Deps{Sessions: session.NewManager(), Channels: channel.NewRegistry()})
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
