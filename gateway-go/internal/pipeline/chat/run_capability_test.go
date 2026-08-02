package chat

import (
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

func capTestRegistry(t *testing.T, providers map[string]modelrole.ProviderResolved) *modelrole.Registry {
	t.Helper()
	// Non-vllm roles everywhere so registry construction performs no network
	// discovery probe.
	return modelrole.NewRegistryWithOptions(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		modelrole.RegistryOptions{
			MainModel:        "acme/custom-model",
			LightweightModel: "acme/custom-model",
			FallbackModel:    "acme/custom-model",
			TinyModel:        "acme/custom-model",
			Providers:        providers,
		},
	)
}

func TestEffectiveContextBudgetReturnsClampedValue(t *testing.T) {
	baseDeps := runDeps{
		contextCfg: ContextConfig{MemoryTokenBudget: 170_000, SystemPromptBudget: 30_000},
		maxTokens:  16_384,
	}

	t.Run("unknown window keeps configured budget", func(t *testing.T) {
		if got := effectiveContextBudget(baseDeps, "zai", "glm-5-turbo", nil); got != 140_000 {
			t.Errorf("budget = %d, want configured 140000", got)
		}
	})

	t.Run("small window clamps budget", func(t *testing.T) {
		deps := baseDeps
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 60_000},
		})
		// 60000 window - 30000 system - 16384 output reserve = 13616
		if got := effectiveContextBudget(deps, "acme", "custom-model", nil); got != 13_616 {
			t.Errorf("budget = %d, want 13616", got)
		}
	})

	t.Run("tiny window hits the floor", func(t *testing.T) {
		deps := baseDeps
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 8_192},
		})
		if got := effectiveContextBudget(deps, "acme", "custom-model", nil); got != minClampedContextBudget {
			t.Errorf("budget = %d, want floor %d", got, minClampedContextBudget)
		}
	})

	t.Run("large window never raises configured budget", func(t *testing.T) {
		deps := baseDeps
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 1_000_000},
		})
		if got := effectiveContextBudget(deps, "acme", "custom-model", nil); got != 140_000 {
			t.Errorf("budget = %d, want configured 140000 (clamp only shrinks)", got)
		}
	})

	t.Run("zero maxTokens uses default output reserve", func(t *testing.T) {
		deps := baseDeps
		deps.maxTokens = 0
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 60_000},
		})
		// 60000 - 30000 - 8192 (default reserve) = 21808
		if got := effectiveContextBudget(deps, "acme", "custom-model", nil); got != 21_808 {
			t.Errorf("budget = %d, want 21808", got)
		}
	})
}

func TestContextWindowCeilingReturnsWindowBasedThreshold(t *testing.T) {
	// Mirrors the production budget: 170K memory - 30K system → 140K configured.
	baseDeps := runDeps{
		contextCfg: ContextConfig{MemoryTokenBudget: 170_000, SystemPromptBudget: 30_000},
		maxTokens:  16_384,
	}

	t.Run("unknown window returns 0", func(t *testing.T) {
		// No registry → unknown window. Callers that can safely rely on the
		// configured budget apply that fallback outside this raw capability helper.
		if got := contextWindowCeiling(baseDeps, "zai", "glm-5-turbo"); got != 0 {
			t.Errorf("ceiling = %d, want 0 for unknown window", got)
		}
	})

	t.Run("large window: ceiling exceeds the budget (defer band exists)", func(t *testing.T) {
		deps := baseDeps
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 1_000_000},
		})
		// 1_000_000 - 30_000 system - 16_384 reserve = 953_616.
		ceiling := contextWindowCeiling(deps, "acme", "custom-model")
		if ceiling != 953_616 {
			t.Errorf("ceiling = %d, want 953616", ceiling)
		}
		budget := effectiveContextBudget(deps, "acme", "custom-model", nil)
		// The deferral precondition: a window clearly larger than the budget means
		// a turn can run raw above the soft threshold without overflowing.
		if ceiling <= budget {
			t.Errorf("ceiling %d must exceed budget %d so deferral is eligible for large-window models", ceiling, budget)
		}
	})

	t.Run("window-limited model: ceiling equals budget (no defer band)", func(t *testing.T) {
		deps := baseDeps
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 60_000},
		})
		// Both compute window - 30000 - 16384 = 13616; the budget clamps to it, so
		// ceiling == budget → ceiling > budget is false → deferral never fires and
		// small-window models keep the synchronous path.
		ceiling := contextWindowCeiling(deps, "acme", "custom-model")
		budget := effectiveContextBudget(deps, "acme", "custom-model", nil)
		if ceiling != 13_616 || budget != 13_616 {
			t.Fatalf("ceiling=%d budget=%d, want both 13616", ceiling, budget)
		}
		if ceiling > budget {
			t.Errorf("ceiling %d must not exceed budget %d for a window-limited model (no defer)", ceiling, budget)
		}
	})

	t.Run("zero maxTokens uses the default output reserve", func(t *testing.T) {
		deps := baseDeps
		deps.maxTokens = 0
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 200_000},
		})
		// 200_000 - 30_000 - 8192 (default reserve) = 161_808.
		if got := contextWindowCeiling(deps, "acme", "custom-model"); got != 161_808 {
			t.Errorf("ceiling = %d, want 161808", got)
		}
	})
}

func TestCompactionDeferralCeiling(t *testing.T) {
	baseDeps := runDeps{
		contextCfg: ContextConfig{MemoryTokenBudget: 170_000, SystemPromptBudget: 30_000},
		maxTokens:  16_384,
	}

	t.Run("unknown window falls back to configured budget", func(t *testing.T) {
		ceiling, ok := compactionDeferralCeiling(baseDeps, "zai", "glm-5-turbo", 140_000)
		if !ok || ceiling != 140_000 {
			t.Fatalf("ceiling=%d ok=%v, want configured budget and eligible", ceiling, ok)
		}
	})

	t.Run("large known window keeps window ceiling", func(t *testing.T) {
		deps := baseDeps
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 1_000_000},
		})
		ceiling, ok := compactionDeferralCeiling(deps, "acme", "custom-model", 140_000)
		if !ok || ceiling != 953_616 {
			t.Fatalf("ceiling=%d ok=%v, want known large-window ceiling and eligible", ceiling, ok)
		}
	})

	t.Run("known window without headroom stays synchronous", func(t *testing.T) {
		deps := baseDeps
		deps.registry = capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"acme": {BaseURL: "https://acme.example/v1", ContextWindow: 60_000},
		})
		ceiling, ok := compactionDeferralCeiling(deps, "acme", "custom-model", 13_616)
		if ok || ceiling != 13_616 {
			t.Fatalf("ceiling=%d ok=%v, want known window-limited path synchronous", ceiling, ok)
		}
	})

	t.Run("zero budget never defers", func(t *testing.T) {
		ceiling, ok := compactionDeferralCeiling(baseDeps, "zai", "glm-5-turbo", 0)
		if ok || ceiling != 0 {
			t.Fatalf("ceiling=%d ok=%v, want no deferral for legacy zero budget", ceiling, ok)
		}
	})
}

func TestApplyModelTuningWithProfileDefaultsAndOverrides(t *testing.T) {
	reg := capTestRegistry(t, map[string]modelrole.ProviderResolved{
		"acme": {BaseURL: "https://acme.example/v1"},
	})
	deps := runDeps{registry: reg}

	t.Run("profile defaults fill unset sampling only", func(t *testing.T) {
		cfg := agent.AgentConfig{MaxTokens: 8192}
		applyModelTuning(&cfg, deps, RunParams{}, "vllm", "qwen3.6-35b")
		if cfg.Temperature == nil || *cfg.Temperature != 0.7 || cfg.TopP == nil || *cfg.TopP != 0.8 {
			t.Errorf("qwen profile not applied: temp=%v topP=%v", cfg.Temperature, cfg.TopP)
		}

		// An explicit request value must never be overwritten.
		explicit := 0.1
		cfg = agent.AgentConfig{MaxTokens: 8192, Temperature: &explicit}
		applyModelTuning(&cfg, deps, RunParams{}, "vllm", "qwen3.6-35b")
		if *cfg.Temperature != 0.1 {
			t.Errorf("explicit temperature overwritten: %v", *cfg.Temperature)
		}
	})

	t.Run("tuned floor raises but never lowers maxTokens", func(t *testing.T) {
		reg.SetTunedMaxTokens("custom-model", 16384)
		defer reg.SetTunedMaxTokens("custom-model", 0)

		cfg := agent.AgentConfig{MaxTokens: 8192}
		applyModelTuning(&cfg, deps, RunParams{}, "acme", "custom-model")
		if cfg.MaxTokens != 16384 {
			t.Errorf("maxTokens = %d, want tuned floor 16384", cfg.MaxTokens)
		}

		cfg = agent.AgentConfig{MaxTokens: 32768}
		applyModelTuning(&cfg, deps, RunParams{}, "acme", "custom-model")
		if cfg.MaxTokens != 32768 {
			t.Errorf("maxTokens = %d, floor must not lower a larger budget", cfg.MaxTokens)
		}

		// Explicit per-request max wins over the tuned floor.
		reqMax := 4096
		cfg = agent.AgentConfig{MaxTokens: 4096}
		applyModelTuning(&cfg, deps, RunParams{MaxTokens: &reqMax}, "acme", "custom-model")
		if cfg.MaxTokens != 4096 {
			t.Errorf("maxTokens = %d, explicit request value must win", cfg.MaxTokens)
		}
	})

	t.Run("nil registry falls back to builtin profile", func(t *testing.T) {
		cfg := agent.AgentConfig{MaxTokens: 8192}
		applyModelTuning(&cfg, runDeps{}, RunParams{}, "vllm", "qwen3.6-35b")
		if cfg.Temperature == nil || *cfg.Temperature != 0.7 {
			t.Errorf("builtin profile not applied without registry: %v", cfg.Temperature)
		}
	})

	t.Run("deepseek-v4 sampling profile applied", func(t *testing.T) {
		// Pins the dsv4 fix: the shipped generation_config is 1.0/1.0, so the
		// gateway must send the recommended 0.6/0.95 itself.
		cfg := agent.AgentConfig{MaxTokens: 8192}
		applyModelTuning(&cfg, deps, RunParams{}, "vllm", "deepseek-v4-flash")
		if cfg.Temperature == nil || *cfg.Temperature != 0.6 || cfg.TopP == nil || *cfg.TopP != 0.95 {
			t.Errorf("dsv4 profile not applied: temp=%v topP=%v", cfg.Temperature, cfg.TopP)
		}
	})

	t.Run("disabled thinking gets the model toggle kwarg", func(t *testing.T) {
		// Session-level "off" (or a cron payload override) produces a bare
		// disabled config; the model's chat_template toggle must be attached
		// here — it is the only per-request off-switch on deepseek-v4.
		cfg := agent.AgentConfig{MaxTokens: 8192, Thinking: &llm.ThinkingConfig{Type: "disabled"}}
		applyModelTuning(&cfg, deps, RunParams{}, "vllm", "deepseek-v4-flash")
		if cfg.Thinking.TemplateKwarg != "thinking" {
			t.Errorf("TemplateKwarg = %q, want \"thinking\"", cfg.Thinking.TemplateKwarg)
		}

		// Models without a toggle keep the kwarg empty (openai.go falls back
		// to its reasoning_effort floor).
		cfg = agent.AgentConfig{MaxTokens: 8192, Thinking: &llm.ThinkingConfig{Type: "disabled"}}
		applyModelTuning(&cfg, deps, RunParams{}, "acme", "custom-model")
		if cfg.Thinking.TemplateKwarg != "" {
			t.Errorf("TemplateKwarg = %q, want empty for non-toggle model", cfg.Thinking.TemplateKwarg)
		}

		// An enabled config is never touched.
		cfg = agent.AgentConfig{MaxTokens: 8192, Thinking: &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}}
		applyModelTuning(&cfg, deps, RunParams{}, "vllm", "deepseek-v4-flash")
		if cfg.Thinking.TemplateKwarg != "" {
			t.Errorf("enabled config must not get a toggle kwarg, got %q", cfg.Thinking.TemplateKwarg)
		}
	})
}

func TestFillDualModeDefaultThinking(t *testing.T) {
	t.Run("dual-mode model with no config gets enabled adaptive", func(t *testing.T) {
		// The 0731 serving default is non-thinking, so nil no longer inherits
		// thinking-on from the template — the model layer must fill it.
		cfg := agent.AgentConfig{MaxTokens: 8192}
		applyModelTuning(&cfg, runDeps{}, RunParams{}, "vllm", "deepseek-v4-flash")
		if cfg.Thinking == nil || cfg.Thinking.Type != "enabled" ||
			cfg.Thinking.BudgetTokens != dualModeDefaultThinkingBudget {
			t.Errorf("Thinking = %+v, want enabled with default budget %d",
				cfg.Thinking, dualModeDefaultThinkingBudget)
		}
	})

	t.Run("wormhole-fronted dual-mode model also gets the default", func(t *testing.T) {
		cfg := agent.AgentConfig{MaxTokens: 8192}
		fillDualModeDefaultThinking(&cfg, runDeps{}, "wormhole", "deepseek-v4-flash")
		if cfg.Thinking == nil || cfg.Thinking.Type != "enabled" {
			t.Errorf("Thinking = %+v, want enabled via wormhole provider", cfg.Thinking)
		}
	})

	t.Run("session-chosen configs are never overridden", func(t *testing.T) {
		// Explicit "off" stays off …
		cfg := agent.AgentConfig{MaxTokens: 8192, Thinking: &llm.ThinkingConfig{Type: "disabled"}}
		applyModelTuning(&cfg, runDeps{}, RunParams{}, "vllm", "deepseek-v4-flash")
		if cfg.Thinking.Type != "disabled" {
			t.Errorf("disabled config overridden: %+v", cfg.Thinking)
		}
		// … and an explicit budget stays as chosen.
		cfg = agent.AgentConfig{MaxTokens: 8192, Thinking: &llm.ThinkingConfig{Type: "enabled", BudgetTokens: 4096}}
		applyModelTuning(&cfg, runDeps{}, RunParams{}, "vllm", "deepseek-v4-flash")
		if cfg.Thinking.BudgetTokens != 4096 {
			t.Errorf("explicit budget overridden: %+v", cfg.Thinking)
		}
	})

	t.Run("models without a template toggle stay nil", func(t *testing.T) {
		// Non-dual-mode local model: no toggle, no default.
		cfg := agent.AgentConfig{MaxTokens: 8192}
		applyModelTuning(&cfg, runDeps{}, RunParams{}, "vllm", "qwen3.6-35b")
		if cfg.Thinking != nil {
			t.Errorf("qwen must stay nil, got %+v", cfg.Thinking)
		}
		// Same model name on a non-vLLM provider: the toggle (and therefore
		// the default) must not leak to cloud providers.
		cfg = agent.AgentConfig{MaxTokens: 8192}
		applyModelTuning(&cfg, runDeps{}, RunParams{}, "acme", "deepseek-v4-flash")
		if cfg.Thinking != nil {
			t.Errorf("cloud-provider dsv4 must stay nil, got %+v", cfg.Thinking)
		}
	})
}

func TestResolveThinkingConfigReturnsDisabledForOffAliases(t *testing.T) {
	for _, level := range []string{"off", "none", "disabled", " OFF "} {
		cfg := resolveThinkingConfig(level)
		if cfg == nil || cfg.Type != "disabled" {
			t.Errorf("resolveThinkingConfig(%q) = %+v, want Type=disabled", level, cfg)
		}
	}
	if cfg := resolveThinkingConfig(""); cfg != nil {
		t.Errorf("empty level must stay nil (provider default), got %+v", cfg)
	}
}

func TestModelCapabilityWithConfigOverridesCacheRejection(t *testing.T) {
	t.Run("builtin without registry", func(t *testing.T) {
		// Kimi accepts markers since K2.7 (live-verified 2026-07-17) — no
		// provider rejects by builtin anymore; only a config override can.
		if modelCapability(runDeps{}, "kimi", "kimi-for-coding").RejectsCacheControl {
			t.Error("kimi must accept cache_control by default since K2.7")
		}
		if modelCapability(runDeps{}, "zai", "glm-5-turbo").RejectsCacheControl {
			t.Error("zai must accept cache_control by default")
		}
	})

	t.Run("config overrides builtin in both directions", func(t *testing.T) {
		yes, no := true, false
		deps := runDeps{registry: capTestRegistry(t, map[string]modelrole.ProviderResolved{
			"kimi": {BaseURL: "https://api.kimi.example/coding", PromptCache: &yes},
			"zai":  {BaseURL: "https://api.z.example/anthropic", PromptCache: &no},
		})}
		if modelCapability(deps, "kimi", "kimi-for-coding").RejectsCacheControl {
			t.Error("promptCache:true must clear the kimi builtin rejection")
		}
		if !modelCapability(deps, "zai", "glm-5-turbo").RejectsCacheControl {
			t.Error("promptCache:false must force the strip for zai")
		}
	})
}
