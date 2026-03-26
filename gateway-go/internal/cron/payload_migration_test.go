package cron

import "testing"

func TestMigrateLegacyCronPayloadMap_ProviderToChannel(t *testing.T) {
	payload := map[string]any{
		"provider": "Telegram",
		"message":  "hi",
	}
	mutated := MigrateLegacyCronPayloadMap(payload)
	if !mutated {
		t.Fatal("expected mutation")
	}
	if payload["channel"] != "telegram" {
		t.Errorf("expected channel=telegram, got %v", payload["channel"])
	}
	if _, has := payload["provider"]; has {
		t.Error("expected provider to be removed")
	}
}

func TestMigrateLegacyCronPayloadMap_ChannelNormalized(t *testing.T) {
	payload := map[string]any{
		"channel": "  TELEGRAM  ",
		"message": "hi",
	}
	mutated := MigrateLegacyCronPayloadMap(payload)
	if !mutated {
		t.Fatal("expected mutation")
	}
	if payload["channel"] != "telegram" {
		t.Errorf("expected channel=telegram, got %v", payload["channel"])
	}
}

func TestMigrateLegacyCronPayloadMap_NoChange(t *testing.T) {
	payload := map[string]any{
		"channel": "telegram",
		"message": "hi",
	}
	mutated := MigrateLegacyCronPayloadMap(payload)
	if mutated {
		t.Fatal("expected no mutation")
	}
}
