package modelpicker

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func TestSetMiniappSessionModelPatchesConversationOnly(t *testing.T) {
	mgr := session.NewManager()
	ctrl := NewController(ControllerConfig{Sessions: mgr})
	ctrl.sessions = mgr

	// Empty picker list → bind is rejected (same allowlist as the global set).
	if _, err := ctrl.setMiniappSessionModel(context.Background(), "client:main:alpha", "kimi/kimi-k2.5"); err == nil {
		t.Fatal("expected unknown model to be rejected")
	}
	if got := mgr.Get("client:main:alpha"); got != nil && got.Model != "" {
		t.Fatalf("rejected bind must not write session.Model, got %q", got.Model)
	}
}

func TestSetMiniappSessionModelClearsOverride(t *testing.T) {
	mgr := session.NewManager()
	ctrl := NewController(ControllerConfig{Sessions: mgr})
	ctrl.sessions = mgr
	model := "kimi/kimi-k2.5"
	mgr.Patch("client:main:alpha", session.PatchFields{Model: &model})

	cleared, err := ctrl.setMiniappSessionModel(context.Background(), "client:main:alpha", "")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared != "" {
		t.Fatalf("cleared id = %q, want empty", cleared)
	}
	got := mgr.Get("client:main:alpha")
	if got == nil || got.Model != "" {
		t.Fatalf("session.Model = %+v, want empty override", got)
	}
}
