package chat

import (
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
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
			AnalysisModel:    "acme/custom-model",
			Providers:        providers,
		},
	)
}

func TestEffectiveContextBudget(t *testing.T) {
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

func TestApplyModelTuning(t *testing.T) {
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
		// here because on deepseek-v4 the reasoning_effort field is a no-op.
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

func TestResolveThinkingConfig_Off(t *testing.T) {
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

func TestModelCapability_PromptCacheOverride(t *testing.T) {
	t.Run("builtin without registry", func(t *testing.T) {
		if !modelCapability(runDeps{}, "kimi", "kimi-for-coding").RejectsCacheControl {
			t.Error("kimi must reject cache_control by default")
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
