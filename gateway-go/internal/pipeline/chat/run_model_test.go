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
		callbacks: CallbackSnapshot{
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

	got := resolveModel(RunParams{SessionKey: "client:main:impl"}, deps, sess)
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
		callbacks: CallbackSnapshot{defaultModel: "zai/glm-main"},
		providerConfigs: map[string]ProviderConfig{
			"zai-subagent": {BaseURL: "https://example.invalid/zai-subagent"},
		},
	}
	sess := &session.Session{
		Model:       "main",
		AgentConfig: session.AgentConfig{SpawnedBy: "client:main"},
	}

	got := resolveModel(RunParams{SessionKey: "client:main:research"}, deps, sess)
	if got.providerID != "zai-subagent" {
		t.Errorf("providerID = %q, want zai-subagent", got.providerID)
	}
	if got.initialRole != modelrole.RoleMain {
		t.Errorf("initialRole = %q, want %q", got.initialRole, modelrole.RoleMain)
	}
}

func TestResolveModelUsesSessionOverrideForDirectChat(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel: "zai/glm-main",
	})
	deps := runDeps{
		registry:  reg,
		callbacks: CallbackSnapshot{defaultModel: "zai/glm-main"},
	}
	sess := &session.Session{Model: "kimi/kimi-k2.5"}

	got := resolveModel(RunParams{SessionKey: "client:main:alpha"}, deps, sess)
	if got.model != "kimi-k2.5" {
		t.Errorf("model = %q, want kimi-k2.5", got.model)
	}
	if got.providerID != "kimi" {
		t.Errorf("providerID = %q, want kimi", got.providerID)
	}
}

func TestResolveModelParamsOverrideSessionModel(t *testing.T) {
	reg := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel: "zai/glm-main",
	})
	deps := runDeps{
		registry:  reg,
		callbacks: CallbackSnapshot{defaultModel: "zai/glm-main"},
	}
	sess := &session.Session{Model: "kimi/kimi-k2.5"}

	got := resolveModel(RunParams{Model: "zai/glm-main"}, deps, sess)
	if got.model != "glm-main" {
		t.Errorf("model = %q, want glm-main (explicit params win)", got.model)
	}
	if got.providerID != "zai" {
		t.Errorf("providerID = %q, want zai", got.providerID)
	}
}
