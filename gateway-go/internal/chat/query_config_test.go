package chat

import (
	"testing"
	"time"
)

func TestDefaultQueryConfig(t *testing.T) {
	cfg := DefaultQueryConfig()
	if cfg.TokenBudget != defaultTokenBudget {
		t.Errorf("TokenBudget = %d, want %d", cfg.TokenBudget, defaultTokenBudget)
	}
	if cfg.MaxTurns != defaultMaxTurns {
		t.Errorf("MaxTurns = %d, want %d", cfg.MaxTurns, defaultMaxTurns)
	}
	if cfg.MaxOutputTokens != defaultMaxTokens {
		t.Errorf("MaxOutputTokens = %d, want %d", cfg.MaxOutputTokens, defaultMaxTokens)
	}
	if cfg.AgentTimeout != defaultAgentTimeout {
		t.Errorf("AgentTimeout = %v, want %v", cfg.AgentTimeout, defaultAgentTimeout)
	}
	if cfg.SnapshotTime.IsZero() {
		t.Error("SnapshotTime should be set")
	}
}

func TestBuildQueryConfig(t *testing.T) {
	maxTok := 4096
	params := RunParams{
		SessionKey: "test-session",
		MaxTokens:  &maxTok,
	}
	cfg := BuildQueryConfig(params, "claude-sonnet-4-20250514", "anthropic", "/workspace")

	if cfg.SessionKey != "test-session" {
		t.Errorf("SessionKey = %q", cfg.SessionKey)
	}
	if cfg.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q", cfg.Model)
	}
	if cfg.ProviderID != "anthropic" {
		t.Errorf("ProviderID = %q", cfg.ProviderID)
	}
	if cfg.MaxOutputTokens != 4096 {
		t.Errorf("MaxOutputTokens = %d, want 4096", cfg.MaxOutputTokens)
	}
	if cfg.WorkspaceDir != "/workspace" {
		t.Errorf("WorkspaceDir = %q", cfg.WorkspaceDir)
	}
	if time.Since(cfg.SnapshotTime) > time.Second {
		t.Error("SnapshotTime too old")
	}
}
