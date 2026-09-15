package server

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

func TestMailAnalysisAgentModelUsesSubmainWhenConfigured(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel:    "main-provider/main-model",
		SubmainModel: "sub-provider/submain-model",
	})
	s := &Server{ChatManager: &ChatManager{modelRegistry: reg}}

	if got := s.mailAnalysisAgentModel(); got != string(modelrole.RoleSubmain) {
		t.Fatalf("mailAnalysisAgentModel() = %q, want %q", got, modelrole.RoleSubmain)
	}
}

func TestMailAnalysisAgentModelFallsBackToDefaultMain(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel: "main-provider/main-model",
	})
	s := &Server{ChatManager: &ChatManager{modelRegistry: reg}}

	if got := s.mailAnalysisAgentModel(); got != "" {
		t.Fatalf("mailAnalysisAgentModel() = %q, want empty default-model sentinel", got)
	}
}

// Stage-1 gets tiny's unmetered chain, each rung tagged with the provider it is
// reached through so its usage is not booked to the stage-1 model's provider.
func TestMailStageOneFallbacksCarryTheirProviderAndSkipBilledRungs(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel:         "wormhole/main-model",
		TinyModel:         "wormhole/tiny-model",
		TinyFallbackModel: "openrouter/nvidia/nemotron-3-super-120b-a12b:free",
		LightweightModel:  "wormhole/deepseek-v4-flash-api",
		FallbackModel:     "wormhole/deepseek-v4-flash-api",
		Providers: map[string]modelrole.ProviderResolved{
			"wormhole":   {BaseURL: "http://127.0.0.1:1/v1", APIKey: "k"},
			"openrouter": {BaseURL: "http://127.0.0.1:2/v1", APIKey: "k"},
		},
		MeteredModels: map[string]bool{"deepseek-v4-flash-api": true},
	})
	s := &Server{ChatManager: &ChatManager{modelRegistry: reg}}

	got := s.mailStageOneFallbacks()
	if len(got) != 1 || got[0].Model != "nvidia/nemotron-3-super-120b-a12b:free" || got[0].Provider != "openrouter" || got[0].Client == nil {
		t.Fatalf("mailStageOneFallbacks() = %+v, want only the free tiny fallback, tagged openrouter", got)
	}
}
