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
