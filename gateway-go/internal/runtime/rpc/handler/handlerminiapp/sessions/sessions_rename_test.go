package sessions

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func TestSessionsRename(t *testing.T) {
	mgr := &fakeSessionsLister{out: []*session.Session{{Key: "client:main:x", Label: "old"}}}
	h := SessionsMethods(SessionsDeps{Manager: mgr})["miniapp.sessions.rename"]

	t.Run("renames and clamps", func(t *testing.T) {
		resp := h(authedCtx(), reqWith(t, "miniapp.sessions.rename", map[string]any{
			"sessionKey": "client:main:x",
			"label":      "  새 제목  ",
		}))
		var payload map[string]any
		decode(t, resp, &payload)
		if payload["renamed"] != true {
			t.Fatalf("renamed = %v, want true", payload["renamed"])
		}
		if mgr.out[0].Label != "새 제목" {
			t.Errorf("label = %q, want trimmed rename", mgr.out[0].Label)
		}
	})

	t.Run("unknown key reports renamed=false", func(t *testing.T) {
		resp := h(authedCtx(), reqWith(t, "miniapp.sessions.rename", map[string]any{
			"sessionKey": "client:missing",
			"label":      "제목",
		}))
		var payload map[string]any
		decode(t, resp, &payload)
		if payload["renamed"] != false {
			t.Fatalf("renamed = %v, want false", payload["renamed"])
		}
	})

	t.Run("blank label rejected", func(t *testing.T) {
		resp := h(authedCtx(), reqWith(t, "miniapp.sessions.rename", map[string]any{
			"sessionKey": "client:main:x",
			"label":      "   ",
		}))
		if resp.Error == nil {
			t.Fatal("want MissingParam error for blank label")
		}
	})
}
