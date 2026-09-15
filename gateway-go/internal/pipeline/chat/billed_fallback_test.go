package chat

import (
	"io"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// A paid OpenRouter model is billed even though the router's metered flag never
// sees it: the chat walk skips it, so it must not count as a healthy fallback.
func TestHealthyFallbackExistsIgnoresPaidOpenRouterRung(t *testing.T) {
	providers := map[string]modelrole.ProviderResolved{
		"openrouter": {BaseURL: "https://openrouter.example/api/v1", APIKey: "k"},
		"zai":        {BaseURL: "https://zai.example/v1", APIKey: "k"},
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	paid := modelrole.NewRegistryWithOptions(quiet, modelrole.RegistryOptions{
		MainModel:        "zai/glm-5.3",
		LightweightModel: "openrouter/deepseek/deepseek-v4-flash",
		FallbackModel:    "openrouter/openai/gpt-oss-120b",
		Providers:        providers,
	})
	if healthyFallbackExists(paid, modelrole.RoleMain, "glm-5.3") {
		t.Fatal("paid OpenRouter rungs counted as a healthy fallback")
	}
	free := modelrole.NewRegistryWithOptions(quiet, modelrole.RegistryOptions{
		MainModel:        "zai/glm-5.3",
		LightweightModel: "openrouter/deepseek/deepseek-v4-flash",
		FallbackModel:    "openrouter/nvidia/nemotron-3-super-120b-a12b:free",
		Providers:        providers,
	})
	if !healthyFallbackExists(free, modelrole.RoleMain, "glm-5.3") {
		t.Fatal("a free OpenRouter rung did not count as a healthy fallback")
	}
}
