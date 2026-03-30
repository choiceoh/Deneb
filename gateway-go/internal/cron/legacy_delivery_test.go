package cron

import "testing"

func TestHasLegacyDeliveryHints(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    bool
	}{
		{"empty", map[string]any{}, false},
		{"deliver true", map[string]any{"deliver": true}, true},
		{"deliver false", map[string]any{"deliver": false}, true},
		{"channel set", map[string]any{"channel": "telegram"}, true},
		{"provider set", map[string]any{"provider": "telegram"}, true},
		{"to set", map[string]any{"to": "12345"}, true},
		{"bestEffort", map[string]any{"bestEffortDeliver": true}, true},
		{"empty channel", map[string]any{"channel": ""}, false},
		{"empty provider", map[string]any{"provider": " "}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasLegacyDeliveryHints(tt.payload)
			if got != tt.want {
				t.Errorf("HasLegacyDeliveryHints(%v) = %v, want %v", tt.payload, got, tt.want)
			}
		})
	}
}

func TestBuildDeliveryFromLegacyPayload(t *testing.T) {
	payload := map[string]any{
		"deliver":  true,
		"provider": "telegram",
		"to":       "12345",
	}
	delivery := BuildDeliveryFromLegacyPayload(payload)
	if delivery["mode"] != "announce" {
		t.Errorf("expected mode=announce, got %v", delivery["mode"])
	}
	if delivery["channel"] != "telegram" {
		t.Errorf("expected channel=telegram, got %v", delivery["channel"])
	}
	if delivery["to"] != "12345" {
		t.Errorf("expected to=12345, got %v", delivery["to"])
	}
}

func TestBuildDeliveryFromLegacyPayload_DeliverFalse(t *testing.T) {
	payload := map[string]any{"deliver": false}
	delivery := BuildDeliveryFromLegacyPayload(payload)
	if delivery["mode"] != "none" {
		t.Errorf("expected mode=none, got %v", delivery["mode"])
	}
}

func TestMergeLegacyDeliveryInto(t *testing.T) {
	existing := map[string]any{"mode": "announce", "channel": "telegram"}
	payload := map[string]any{"channel": "telegram", "to": "99"}
	merged, mutated := MergeLegacyDeliveryInto(existing, payload)
	if !mutated {
		t.Fatal("expected mutation")
	}
	if merged["channel"] != "telegram" {
		t.Errorf("expected channel=telegram, got %v", merged["channel"])
	}
	if merged["to"] != "99" {
		t.Errorf("expected to=99, got %v", merged["to"])
	}
	// Original should not be modified.
	if existing["channel"] != "telegram" {
		t.Error("expected original unchanged")
	}
}

func TestStripLegacyDeliveryFieldsFromPayload(t *testing.T) {
	payload := map[string]any{
		"deliver":           true,
		"channel":           "telegram",
		"provider":          "telegram",
		"to":                "12345",
		"bestEffortDeliver": true,
		"message":           "keep this",
	}
	StripLegacyDeliveryFieldsFromPayload(payload)
	for _, field := range []string{"deliver", "channel", "provider", "to", "bestEffortDeliver"} {
		if _, has := payload[field]; has {
			t.Errorf("expected %s to be removed", field)
		}
	}
	if payload["message"] != "keep this" {
		t.Error("expected message to remain")
	}
}
