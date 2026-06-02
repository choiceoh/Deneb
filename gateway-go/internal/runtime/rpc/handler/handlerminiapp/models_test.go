package handlerminiapp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestModelsList_WithInitData(t *testing.T) {
	h := modelsList(ModelDeps{
		CurrentModel: func() string { return "zai/glm-5.1" },
		ListModels: func(context.Context) ([]ModelSection, error) {
			return []ModelSection{{
				Title: "Z.ai",
				Models: []ModelOption{{
					ID:       "zai/glm-5.1",
					Label:    "glm-5.1",
					Provider: "zai",
					Display:  "glm-5.1",
					Current:  true,
				}},
			}}, nil
		},
	})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())

	got := decodePayload(t, h(ctx, newReq(t, "miniapp.models.list")))
	if got["current"] != "zai/glm-5.1" {
		t.Errorf("current = %v, want zai/glm-5.1", got["current"])
	}
	sections, ok := got["sections"].([]any)
	if !ok || len(sections) != 1 {
		t.Fatalf("sections = %#v, want one section", got["sections"])
	}
}

func TestModelsList_NoInitData(t *testing.T) {
	h := modelsList(ModelDeps{
		ListModels: func(context.Context) ([]ModelSection, error) {
			t.Fatal("ListModels should not be called without initData")
			return nil, nil
		},
	})

	resp := h(context.Background(), newReq(t, "miniapp.models.list"))
	if resp.OK {
		t.Fatal("expected unauthorized response")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestModelsSet_WithInitData(t *testing.T) {
	var requested, gotRole string
	h := modelsSet(ModelDeps{
		SetModel: func(_ context.Context, role, id string) (string, error) {
			gotRole = role
			requested = id
			return "zai/glm-5.1", nil
		},
	})
	req := reqWith(t, "miniapp.models.set", map[string]any{"id": " zai/glm-5.1 "})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())

	got := decodePayload(t, h(ctx, req))
	if requested != "zai/glm-5.1" {
		t.Errorf("requested = %q, want trimmed id", requested)
	}
	if gotRole != "main" {
		t.Errorf("role = %q, want main (default when omitted)", gotRole)
	}
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["current"] != "zai/glm-5.1" {
		t.Errorf("current = %v, want zai/glm-5.1", got["current"])
	}
}

func TestModelsSet_WithRole(t *testing.T) {
	var gotRole, gotID string
	h := modelsSet(ModelDeps{
		SetModel: func(_ context.Context, role, id string) (string, error) {
			gotRole, gotID = role, id
			return id, nil
		},
	})
	req := reqWith(t, "miniapp.models.set", map[string]any{"id": "vllm/qwen", "role": "lightweight"})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())

	got := decodePayload(t, h(ctx, req))
	if gotRole != "lightweight" {
		t.Errorf("role = %q, want lightweight", gotRole)
	}
	if gotID != "vllm/qwen" {
		t.Errorf("id = %q, want vllm/qwen", gotID)
	}
	if got["role"] != "lightweight" {
		t.Errorf("response role = %v, want lightweight", got["role"])
	}
}

func TestModelsSet_MissingID(t *testing.T) {
	h := modelsSet(ModelDeps{})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())
	resp := h(ctx, reqWith(t, "miniapp.models.set", map[string]any{"id": "   "}))

	if resp.OK {
		t.Fatal("expected missing param response")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestModelsAddCustom_WithInitData(t *testing.T) {
	var gotEndpoint, gotModel string
	h := modelsAddCustom(ModelDeps{
		AddModel: func(_ context.Context, endpoint, model string) (ModelAddResult, error) {
			gotEndpoint = endpoint
			gotModel = model
			return ModelAddResult{
				ID:       "custom/qwen3.6-35b-a3b",
				Provider: "custom",
				Endpoint: "http://127.0.0.1:8000/v1",
				Model:    "qwen3.6-35b-a3b",
				Added:    true,
			}, nil
		},
	})
	req := reqWith(t, "miniapp.models.add_custom", map[string]any{
		"endpoint": " http://127.0.0.1:8000/v1 ",
		"model":    " qwen3.6-35b-a3b ",
	})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())

	got := decodePayload(t, h(ctx, req))
	if gotEndpoint != "http://127.0.0.1:8000/v1" {
		t.Errorf("endpoint = %q, want trimmed endpoint", gotEndpoint)
	}
	if gotModel != "qwen3.6-35b-a3b" {
		t.Errorf("model = %q, want trimmed model", gotModel)
	}
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["id"] != "custom/qwen3.6-35b-a3b" {
		t.Errorf("id = %v, want custom/qwen3.6-35b-a3b", got["id"])
	}
	if got["added"] != true {
		t.Errorf("added = %v, want true", got["added"])
	}
}

func TestModelsAddCustom_MissingEndpoint(t *testing.T) {
	h := modelsAddCustom(ModelDeps{})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())
	resp := h(ctx, reqWith(t, "miniapp.models.add_custom", map[string]any{
		"endpoint": "   ",
		"model":    "qwen3.6-35b-a3b",
	}))

	if resp.OK {
		t.Fatal("expected missing param response")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestModelsAddCustom_MissingModel(t *testing.T) {
	h := modelsAddCustom(ModelDeps{})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())
	resp := h(ctx, reqWith(t, "miniapp.models.add_custom", map[string]any{
		"endpoint": "http://127.0.0.1:8000/v1",
		"model":    "   ",
	}))

	if resp.OK {
		t.Fatal("expected missing param response")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestModelsDeleteCustom_WithInitData(t *testing.T) {
	var gotID string
	h := modelsDeleteCustom(ModelDeps{
		DeleteModel: func(_ context.Context, id string) (ModelDeleteResult, error) {
			gotID = id
			return ModelDeleteResult{
				ID:           "custom/typo-model",
				Removed:      true,
				ClearedRoles: []string{"main"},
				Current:      "vllm/gemma4",
			}, nil
		},
	})
	req := reqWith(t, "miniapp.models.delete_custom", map[string]any{"id": " custom/typo-model "})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())

	got := decodePayload(t, h(ctx, req))
	if gotID != "custom/typo-model" {
		t.Errorf("id = %q, want trimmed id", gotID)
	}
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["removed"] != true {
		t.Errorf("removed = %v, want true", got["removed"])
	}
	if got["current"] != "vllm/gemma4" {
		t.Errorf("current = %v, want vllm/gemma4", got["current"])
	}
	roles, ok := got["clearedRoles"].([]any)
	if !ok || len(roles) != 1 || roles[0] != "main" {
		t.Errorf("clearedRoles = %#v, want [main]", got["clearedRoles"])
	}
}

func TestModelsDeleteCustom_MissingID(t *testing.T) {
	h := modelsDeleteCustom(ModelDeps{})
	ctx := clientauth.WithContext(context.Background(), sampleInitData())
	resp := h(ctx, reqWith(t, "miniapp.models.delete_custom", map[string]any{"id": "   "}))

	if resp.OK {
		t.Fatal("expected missing param response")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestModelsDeleteCustom_NoInitData(t *testing.T) {
	h := modelsDeleteCustom(ModelDeps{
		DeleteModel: func(context.Context, string) (ModelDeleteResult, error) {
			t.Fatal("DeleteModel should not be called without initData")
			return ModelDeleteResult{}, nil
		},
	})

	resp := h(context.Background(), reqWith(t, "miniapp.models.delete_custom", map[string]any{"id": "custom/x"}))
	if resp.OK {
		t.Fatal("expected unauthorized response")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}
