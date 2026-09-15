package modelrole

import (
	"encoding/json"
	"testing"
)

// TestThinkingOffDirectiveReturnsPerModelToggle pins the shared raw-call
// thinking-off policy consumed by the localai hub, the pilot direct path,
// and the wiki dreamer wiring. The dual-mode case is the load-bearing one:
// deepseek-v4 keeps Profile.Reasoning=false by design, so a reasoning-first
// check would send it the Qwen enable_thinking spelling its template
// ignores.
func TestThinkingOffDirectiveReturnsPerModelToggle(t *testing.T) {
	toggleOf := func(directive *ThinkingOffDirective) map[string]bool {
		t.Helper()
		if directive == nil {
			t.Fatal("directive = nil, want template toggle")
		}
		return map[string]bool{directive.TemplateKwarg(): false}
	}

	t.Run("dsv4 on vLLM-backed providers gets its template toggle", func(t *testing.T) {
		for _, provider := range []string{"vllm", "wormhole"} {
			ctk := toggleOf(ThinkingOffDirectiveFor(provider, "deepseek-v4-flash"))
			if got, ok := ctk["thinking"]; !ok || got {
				t.Errorf("%s: thinking = %v, present=%v; want false and present", provider, got, ok)
			}
			if _, has := ctk["enable_thinking"]; has {
				t.Errorf("%s: enable_thinking must not be sent to dsv4", provider)
			}
		}
	})

	t.Run("untoggleable reasoning models get nil (budget for thinking)", func(t *testing.T) {
		for _, model := range []string{"qwen3.6-35b-a3b", "deepseek-r1", "step3p7", "deepseek-v4-flash-api"} {
			if got := ThinkingOffDirectiveFor("vllm", model); got != nil {
				t.Errorf("%s: directive = %v, want nil", model, got)
			}
		}
	})

	t.Run("non-reasoning models keep the enable_thinking directive", func(t *testing.T) {
		for _, model := range []string{"gemma4", "qwen3.6-35b-instruct"} {
			ctk := toggleOf(ThinkingOffDirectiveFor("vllm", model))
			if got, ok := ctk["enable_thinking"]; !ok || got {
				t.Errorf("%s: enable_thinking = %v, present=%v; want false and present", model, got, ok)
			}
		}
	})

	t.Run("direct cloud providers are left unshaped", func(t *testing.T) {
		// chat_template_kwargs is a vLLM serving feature: a strict OpenAI-compat
		// API can 400 on the unknown field, so off vLLM-backed providers the
		// three-way now returns nil for non-reasoning models too (the hub's
		// historical shared raw map used to leak to cloud lightweight configs).
		for _, model := range []string{"deepseek-v4-flash", "gemma4"} {
			if got := ThinkingOffDirectiveFor("zai", model); got != nil {
				t.Errorf("zai/%s: directive = %v, want nil off vLLM-backed providers", model, got)
			}
		}
	})
}

// TestThinkingOffDirectiveForRoleForcesTinyOff pins the role-level policy: the
// tiny role (speed/concurrency over quality) forces thinking off even for a
// reasoning model the per-model policy leaves on, while other roles keep the
// per-model decision.
func TestThinkingOffDirectiveForRoleForcesTinyOff(t *testing.T) {
	var reg *Registry // nil registry → package heuristics; the role force still applies

	// Reasoning model the per-model policy leaves on (nil): tiny forces it off with
	// the standard vLLM toggle; a non-forcing role keeps thinking on.
	if got := ThinkingOffDirectiveFor("wormhole", "qwen3.6-35b-a3b"); got != nil {
		t.Fatalf("precondition: per-model directive for qwen3.6 = %v, want nil", got)
	}
	if d := reg.ThinkingOffDirectiveForRole(RoleTiny, "wormhole", "qwen3.6-35b-a3b"); d == nil || d.TemplateKwarg() != "enable_thinking" {
		t.Errorf("tiny/qwen3.6 = %v, want enable_thinking", d)
	}
	if d := reg.ThinkingOffDirectiveForRole(RoleMain, "wormhole", "qwen3.6-35b-a3b"); d != nil {
		t.Errorf("main/qwen3.6 = %v, want nil (per-model policy, thinking stays on)", d)
	}

	// The per-model toggle still wins for tiny — dsv4 keeps its own spelling.
	if d := reg.ThinkingOffDirectiveForRole(RoleTiny, "wormhole", "deepseek-v4-flash"); d == nil || d.TemplateKwarg() != "thinking" {
		t.Errorf("tiny/dsv4 = %v, want thinking", d)
	}

	// Off vLLM-backed providers chat_template_kwargs is unsupported, so tiny never
	// gets the kwarg there. A provider with no off-switch at all stays nil even
	// for tiny; OpenRouter has its own field and gets that instead.
	if d := reg.ThinkingOffDirectiveForRole(RoleTiny, "zai", "qwen3.6-35b-a3b"); d != nil {
		t.Errorf("tiny/zai = %v, want nil: no supported off-switch", d)
	}
	if d := reg.ThinkingOffDirectiveForRole(RoleTiny, "openrouter", "qwen3.6-35b-a3b"); d == nil || d.TemplateKwarg() != "" || !d.DisablesReasoningParam() {
		t.Errorf("tiny/openrouter = %v, want the reasoning field and no template kwarg", d)
	}
}

func TestThinkingOffDirectivePreservesWireShape(t *testing.T) {
	for _, test := range []struct {
		name     string
		model    string
		wantJSON string
	}{
		{name: "dual mode", model: "deepseek-v4-flash", wantJSON: `{"chat_template_kwargs":{"thinking":false}}`},
		{name: "non reasoning", model: "gemma4", wantJSON: `{"chat_template_kwargs":{"enable_thinking":false}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			directive := ThinkingOffDirectiveFor("vllm", test.model)
			if directive == nil {
				t.Fatal("directive = nil")
			}
			body := map[string]any{
				"chat_template_kwargs": map[string]bool{directive.TemplateKwarg(): false},
			}
			got, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal wire body: %v", err)
			}
			if string(got) != test.wantJSON {
				t.Errorf("wire body = %s, want %s", got, test.wantJSON)
			}
		})
	}
}

// OpenRouter takes the switch as its own request field; a chat_template_kwargs
// toggle never reaches the model behind the hosting provider. Measured
// 2026-09-15: nemotron-3-super without reasoning.enabled=false wrote its
// reasoning into the content and exhausted a 32-token title budget.
func TestThinkingOffDirectiveUsesReasoningParamOnOpenRouter(t *testing.T) {
	d := ThinkingOffDirectiveFor("openrouter", "nvidia/nemotron-3-super-120b-a12b:free")
	if d == nil || !d.DisablesReasoningParam() || d.TemplateKwarg() != "" {
		t.Fatalf("openrouter non-reasoning directive = %+v, want the reasoning-param kind", d)
	}
	// vLLM-backed servings keep the template kwarg, byte-identical to before.
	if v := ThinkingOffDirectiveFor("vllm", "gemma4"); v == nil || v.DisablesReasoningParam() || v.TemplateKwarg() != "enable_thinking" {
		t.Fatalf("vllm directive = %+v, want the enable_thinking kwarg", v)
	}
	// Direct cloud providers without the field still get nothing.
	if z := ThinkingOffDirectiveFor("zai", "glm-5.3"); z != nil {
		t.Fatalf("zai directive = %+v, want nil", z)
	}
}

func TestThinkingOffDirectiveForRoleForcesReasoningParamForTinyRoles(t *testing.T) {
	reasoning := "deepseek-r1" // a reasoning model: the per-model policy leaves it on
	if ThinkingOffDirectiveFor("openrouter", reasoning) != nil {
		t.Fatal("precondition: the per-model policy must leave a reasoning model alone")
	}
	var reg *Registry
	for _, role := range []Role{RoleTiny, RoleTinyFallback} {
		d := reg.ThinkingOffDirectiveForRole(role, "openrouter", reasoning)
		if d == nil || !d.DisablesReasoningParam() {
			t.Fatalf("role %s on openrouter = %+v, want thinking forced off via the reasoning field", role, d)
		}
	}
	if d := reg.ThinkingOffDirectiveForRole(RoleLightweight, "openrouter", reasoning); d != nil {
		t.Fatalf("lightweight on openrouter = %+v, want nil (only speed-first roles force it)", d)
	}
}
