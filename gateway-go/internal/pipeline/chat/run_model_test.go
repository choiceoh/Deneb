package chat

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func TestResolveModelReturnsCodingModelForSubagent(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel:   "zai/glm-main",
		CodingModel: "kimi/kimi-for-coding",
	})
	deps := runDeps{
		registry: reg,
		callbacks: callbackSnapshot{
			defaultModel: "zai/glm-main",
		},
		providerConfigs: map[string]ProviderConfig{
			"kimi-subagent": {BaseURL: "https://example.invalid/kimi-subagent"},
		},
	}
	sess := &session.Session{
		Model:       "coding",
		AgentConfig: session.AgentConfig{SpawnedBy: "client:main"},
	}

	got := resolveModel(runParams{SessionKey: "client:main:impl"}, deps, sess)
	if got.model != "kimi-for-coding" {
		t.Errorf("model = %q, want kimi-for-coding", got.model)
	}
	if got.providerID != "kimi" {
		t.Errorf("providerID = %q, want kimi", got.providerID)
	}
	if got.initialRole != modelrole.RoleCoding {
		t.Errorf("initialRole = %q, want %q", got.initialRole, modelrole.RoleCoding)
	}
}

func TestResolveModelReturnsRemappedProviderForNonCodingSubagent(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel: "zai/glm-main",
	})
	deps := runDeps{
		registry:  reg,
		callbacks: callbackSnapshot{defaultModel: "zai/glm-main"},
		providerConfigs: map[string]ProviderConfig{
			"zai-subagent": {BaseURL: "https://example.invalid/zai-subagent"},
		},
	}
	sess := &session.Session{
		Model:       "main",
		AgentConfig: session.AgentConfig{SpawnedBy: "client:main"},
	}

	got := resolveModel(runParams{SessionKey: "client:main:research"}, deps, sess)
	if got.providerID != "zai-subagent" {
		t.Errorf("providerID = %q, want zai-subagent", got.providerID)
	}
	if got.initialRole != modelrole.RoleMain {
		t.Errorf("initialRole = %q, want %q", got.initialRole, modelrole.RoleMain)
	}
}
