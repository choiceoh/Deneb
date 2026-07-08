package chat

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// TestResolveModel_CodingSession verifies that a 코드모드 (code:) session routes
// to RoleCoding when an operator configured a coding model, and falls back to
// the main model when the role is unconfigured.
func TestResolveModel_CodingSession(t *testing.T) {
	withCoding := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel:   "zai/glm-main",
		CodingModel: "kimi/kimi-for-coding",
	})
	noCoding := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel: "zai/glm-main",
	})

	cases := []struct {
		name         string
		registry     *modelrole.Registry
		sessionKey   string
		wantModel    string
		wantProvider string
		wantRole     modelrole.Role
	}{
		{
			name:         "coding session uses coding role when configured",
			registry:     withCoding,
			sessionKey:   "code:task-0701-1",
			wantModel:    "kimi-for-coding",
			wantProvider: "kimi",
			wantRole:     modelrole.RoleCoding,
		},
		{
			name:         "work session keeps the main model",
			registry:     withCoding,
			sessionKey:   "client:main",
			wantModel:    "glm-main",
			wantProvider: "zai",
			wantRole:     modelrole.RoleMain,
		},
		{
			name:         "coding session falls back to main when no coding model configured",
			registry:     noCoding,
			sessionKey:   "code:task-0701-1",
			wantModel:    "glm-main",
			wantProvider: "zai",
			wantRole:     modelrole.RoleMain,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := runDeps{
				registry:  tc.registry,
				callbacks: CallbackSnapshot{defaultModel: "zai/glm-main"},
			}
			got := resolveModel(RunParams{SessionKey: tc.sessionKey}, deps, nil)
			if got.model != tc.wantModel {
				t.Errorf("model = %q, want %q", got.model, tc.wantModel)
			}
			if got.providerID != tc.wantProvider {
				t.Errorf("providerID = %q, want %q", got.providerID, tc.wantProvider)
			}
			if got.initialRole != tc.wantRole {
				t.Errorf("initialRole = %q, want %q", got.initialRole, tc.wantRole)
			}
		})
	}
}

func TestResolveModel_SubagentCodingRole(t *testing.T) {
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

func TestResolveModel_SubagentRemapsNonCodingProvider(t *testing.T) {
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
