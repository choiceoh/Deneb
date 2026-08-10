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
