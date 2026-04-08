package telegram

import (
	"encoding/json"
	"testing"
)

func TestConfig_UnmarshalJSON(t *testing.T) {
	input := `{
		"botToken": "123:ABC",
		"chatID": 42,
		"timeoutSeconds": 60
	}`

	var c Config
	if err := json.Unmarshal([]byte(input), &c); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if c.BotToken != "123:ABC" {
		t.Errorf("botToken: got %q", c.BotToken)
	}
	if c.ChatID != 42 {
		t.Errorf("chatID: got %d", c.ChatID)
	}
	if c.EffectiveTimeout() != 60 {
		t.Errorf("timeout: got %d", c.EffectiveTimeout())
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := &Config{}

	if !cfg.IsEnabled() {
		t.Error("expected enabled by default")
	}
	if cfg.IsBlockStreamingDisabled() {
		t.Error("expected block streaming not disabled by default")
	}
	if cfg.EffectiveTimeout() != 30 {
		t.Errorf("expected default timeout 30, got %d", cfg.EffectiveTimeout())
	}
}

func boolPtr(b bool) *bool { return &b }

func TestConfig_Overrides(t *testing.T) {
	cfg := &Config{
		Enabled:        boolPtr(false),
		BlockStreaming: boolPtr(true),
	}

	if cfg.IsEnabled() {
		t.Error("expected disabled")
	}
	if !cfg.IsBlockStreamingDisabled() {
		t.Error("expected block streaming disabled when set to true")
	}
}
