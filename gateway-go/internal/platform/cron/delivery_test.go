package cron

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestResolveDeliveryTarget_ThreadID is the cron-defined-in-topic happy
// path: when a job's stored Delivery has a ThreadID, the resolved
// DeliveryTarget must carry it forward so executeJobFullWithTrigger →
// AgentTurnParams → DeliverCronOutput → SendMessage all see the right
// topic. Without this, cron output from a topic-scoped job silently
// leaks into the supergroup's General topic — the exact regression M4
// is preventing.
func TestResolveDeliveryTarget_ThreadID(t *testing.T) {
	target, err := ResolveDeliveryTarget(
		&JobDeliveryConfig{Channel: "telegram", To: "-1001234567890", ThreadID: "42"},
		"telegram", "fallback-chat")
	if err != nil {
		t.Fatalf("ResolveDeliveryTarget: %v", err)
	}
	if target.ThreadID != "42" {
		t.Errorf("ThreadID = %q, want %q", target.ThreadID, "42")
	}
}

// TestResolveDeliveryTarget_NoThreadID covers the legacy 1:1 path: an
// older job stored before M4 has no ThreadID — the resolved target must
// stay empty, not pick up some default from the defaults pair.
func TestResolveDeliveryTarget_NoThreadID(t *testing.T) {
	target, err := ResolveDeliveryTarget(
		&JobDeliveryConfig{Channel: "telegram", To: "7074071666"},
		"telegram", "fallback-chat")
	if err != nil {
		t.Fatalf("ResolveDeliveryTarget: %v", err)
	}
	if target.ThreadID != "" {
		t.Errorf("ThreadID = %q, want empty", target.ThreadID)
	}
}

func TestNormalizeDeliveryTarget(t *testing.T) {
	tests := []struct {
		ch   string
		to   string
		want string
	}{
		{"telegram", "12345", "12345"},
		{"telegram", " 12345 ", "12345"},
		{"telegram", "user:abc", "user:abc"},
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
		{"wrong provider", "slack", "12345", "", "telegram", "12345", "", false},
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
			"telegram", "99999",
		)
		testutil.NoError(t, err)
		if target.Channel != "telegram" || target.To != "12345" {
			t.Errorf("got %s/%s, want telegram/12345", target.Channel, target.To)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		target := testutil.Must(ResolveDeliveryTarget(nil, "telegram", "12345"))
		if target.Channel != "telegram" || target.To != "12345" {
			t.Errorf("got %s/%s, want defaults", target.Channel, target.To)
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
