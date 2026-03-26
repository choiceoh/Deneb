package cron

import "testing"

func TestNormalizeDeliveryTarget(t *testing.T) {
	tests := []struct {
		ch   string
		to   string
		want string
	}{
		{"telegram", "12345", "12345"},
		{"feishu", "user:abc", "abc"},
		{"lark", "chat:xyz", "xyz"},
		{"feishu", "normal", "normal"},
		{"discord", "user:abc", "user:abc"}, // only feishu/lark strip prefixes
	}
	for _, tt := range tests {
		got := NormalizeDeliveryTarget(tt.ch, tt.to)
		if got != tt.want {
			t.Errorf("NormalizeDeliveryTarget(%q, %q) = %q, want %q", tt.ch, tt.to, got, tt.want)
		}
	}
}

func TestMatchesDeliveryTarget(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		targetTo  string
		targetAcc string
		ch        string
		delivTo   string
		delivAcc  string
		want      bool
	}{
		{"exact match", "telegram", "12345", "", "telegram", "12345", "", true},
		{"provider message", "message", "12345", "", "telegram", "12345", "", true},
		{"wrong provider", "discord", "12345", "", "telegram", "12345", "", false},
		{"wrong target", "telegram", "12345", "", "telegram", "67890", "", false},
		{"with topic suffix", "telegram", "12345:topic:99", "", "telegram", "12345", "", true},
		{"account mismatch", "telegram", "12345", "acc1", "telegram", "12345", "acc2", false},
		{"empty delivery", "", "", "", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesDeliveryTarget(tt.provider, tt.targetTo, tt.targetAcc, tt.ch, tt.delivTo, tt.delivAcc)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveDeliveryTarget(t *testing.T) {
	t.Run("explicit delivery", func(t *testing.T) {
		target, err := ResolveDeliveryTarget(
			&JobDeliveryConfig{Channel: "telegram", To: "12345"},
			"discord", "99999",
		)
		if err != nil {
			t.Fatal(err)
		}
		if target.Channel != "telegram" || target.To != "12345" {
			t.Errorf("expected telegram/12345, got %s/%s", target.Channel, target.To)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		target, err := ResolveDeliveryTarget(nil, "telegram", "12345")
		if err != nil {
			t.Fatal(err)
		}
		if target.Channel != "telegram" || target.To != "12345" {
			t.Errorf("expected defaults, got %s/%s", target.Channel, target.To)
		}
	})

	t.Run("no channel error", func(t *testing.T) {
		_, err := ResolveDeliveryTarget(nil, "", "12345")
		if err == nil {
			t.Error("expected error for missing channel")
		}
	})

	t.Run("no recipient error", func(t *testing.T) {
		_, err := ResolveDeliveryTarget(nil, "telegram", "")
		if err == nil {
			t.Error("expected error for missing recipient")
		}
	})
}
