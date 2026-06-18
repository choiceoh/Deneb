package chat

import (
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// TestResolveModel_ChatbotWorkspace verifies that a 챗봇 (chat:) session routes
// to RoleChatbot when an operator configured a chatbot model, while 업무
// (client:) sessions keep the main model — and that an unconfigured chatbot role
// is a no-op (챗봇 turns fall back to the main model).
func TestResolveModel_ChatbotWorkspace(t *testing.T) {
	// Non-vllm providers so registry construction does not probe a local vLLM.
	withChatbot := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
		MainModel:    "zai/glm-main",
		ChatbotModel: "google/gemini-chatbot",
	})
	noChatbot := modelrole.NewRegistryWithOptions(slog.Default(), modelrole.RegistryOptions{
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
			name:         "chatbot session uses chatbot role when configured",
			registry:     withChatbot,
			sessionKey:   "chat:main",
			wantModel:    "gemini-chatbot",
			wantProvider: "google",
			wantRole:     modelrole.RoleChatbot,
		},
		{
			name:         "chatbot sub-session also uses chatbot role",
			registry:     withChatbot,
			sessionKey:   "chat:main:42",
			wantModel:    "gemini-chatbot",
			wantProvider: "google",
			wantRole:     modelrole.RoleChatbot,
		},
		{
			name:         "work session keeps the main model",
			registry:     withChatbot,
			sessionKey:   "client:main",
			wantModel:    "glm-main",
			wantProvider: "zai",
			wantRole:     modelrole.RoleMain,
		},
		{
			name:         "chatbot session falls back to main when no chatbot model configured",
			registry:     noChatbot,
			sessionKey:   "chat:main",
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
		registry:  reg,
		callbacks: CallbackSnapshot{defaultModel: "zai/glm-main"},
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
