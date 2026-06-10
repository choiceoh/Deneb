package chat

import (
	"io"
	"log/slog"
	"testing"

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
