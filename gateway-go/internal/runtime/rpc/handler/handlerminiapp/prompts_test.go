package handlerminiapp

import (
	"context"
	"path/filepath"
	"testing"

	domainprompts "github.com/choiceoh/deneb/gateway-go/internal/domain/prompts"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func promptTestStore(t *testing.T) *domainprompts.Store {
	t.Helper()
	return domainprompts.NewStore(filepath.Join(t.TempDir(), "prompt-overrides.json"), []domainprompts.Template{{
		ID:          "mail.auto.analysis",
		Title:       "자동 메일 분석",
		Description: "메일 분석 지침",
		Category:    "메일",
		DefaultText: "default prompt",
		Editable:    true,
	}})
}

func TestPromptMethods_ListUpdateReset(t *testing.T) {
	store := promptTestStore(t)
	methods := PromptMethods(PromptDeps{Store: store})
	if methods["miniapp.prompts.list"] == nil || methods["miniapp.prompts.get"] == nil ||
		methods["miniapp.prompts.update"] == nil || methods["miniapp.prompts.reset"] == nil {
		t.Fatalf("PromptMethods missing method: %#v", methods)
	}

	var list PromptListResponse
	decode(t, methods["miniapp.prompts.list"](authedCtx(), reqWith(t, "miniapp.prompts.list", map[string]any{})), &list)
	if list.Count != 1 || len(list.Prompts) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if list.Prompts[0].ID != "mail.auto.analysis" || list.Prompts[0].Overridden {
		t.Fatalf("list row = %+v", list.Prompts[0])
	}

	var updated PromptDetailOut
	decode(t, methods["miniapp.prompts.update"](
		authedCtx(),
		reqWith(t, "miniapp.prompts.update", map[string]any{"id": "mail.auto.analysis", "text": "custom prompt"}),
	), &updated)
	if updated.Text != "custom prompt" || !updated.Overridden || updated.DefaultText != "default prompt" {
		t.Fatalf("updated = %+v", updated)
	}

	var detail PromptDetailOut
	decode(t, methods["miniapp.prompts.get"](
		authedCtx(),
		reqWith(t, "miniapp.prompts.get", map[string]any{"id": "mail.auto.analysis"}),
	), &detail)
	if detail.Text != "custom prompt" || !detail.Overridden {
		t.Fatalf("detail = %+v", detail)
	}

	var reset PromptDetailOut
	decode(t, methods["miniapp.prompts.reset"](
		authedCtx(),
		reqWith(t, "miniapp.prompts.reset", map[string]any{"id": "mail.auto.analysis"}),
	), &reset)
	if reset.Text != "default prompt" || reset.Overridden {
		t.Fatalf("reset = %+v", reset)
	}
}

func TestPromptMethods_Errors(t *testing.T) {
	store := promptTestStore(t)
	methods := PromptMethods(PromptDeps{Store: store})

	resp := methods["miniapp.prompts.get"](authedCtx(), reqWith(t, "miniapp.prompts.get", map[string]any{"id": "missing"}))
	if resp.OK || resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("missing get resp = %+v", resp)
	}

	resp = methods["miniapp.prompts.update"](authedCtx(), reqWith(t, "miniapp.prompts.update", map[string]any{"id": "mail.auto.analysis", "text": " "}))
	if resp.OK || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("empty update resp = %+v", resp)
	}

	resp = methods["miniapp.prompts.list"](context.Background(), reqWith(t, "miniapp.prompts.list", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("unauth list resp = %+v", resp)
	}
}

func TestPromptMethods_NilStoreReturnsNil(t *testing.T) {
	if got := PromptMethods(PromptDeps{}); got != nil {
		t.Fatalf("PromptMethods(nil) = %#v, want nil", got)
	}
}
