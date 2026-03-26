package autoreply

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func TestBuildStatusMessage_Basic(t *testing.T) {
	report := StatusReport{
		SessionKey: "telegram:123",
		Model:      "claude-sonnet-4-20250514",
		Provider:   "anthropic",
		ThinkLevel: types.ThinkHigh,
		Channel:    "telegram",
	}
	msg := BuildStatusMessage(report)
	if !strings.Contains(msg, "telegram:123") {
		t.Error("missing session key")
	}
	if !strings.Contains(msg, "claude-sonnet") {
		t.Error("missing model")
	}
	if !strings.Contains(msg, "Think: high") {
		t.Error("missing think level")
	}
}

func TestBuildStatusMessage_ServerLevel(t *testing.T) {
	report := StatusReport{
		SessionKey:    "telegram:123",
		Model:         "claude-sonnet-4-20250514",
		Provider:      "anthropic",
		Channel:       "telegram",
		Version:       "3.11.4",
		StartedAt:     time.Now().Add(-2*time.Hour - 30*time.Minute),
		RustFFI:       true,
		SessionCount:  1,
		WSConnections: 0,
		ProviderUsage: map[string]*ProviderUsageStats{
			"anthropic": {Calls: 142, Input: 890_000, Output: 310_000},
		},
		ChannelHealth: []ChannelHealthEntry{
			{ID: "Telegram", Healthy: true},
		},
	}
	msg := BuildStatusMessage(report)

	checks := []struct {
		label    string
		fragment string
	}{
		{"version", "Gateway v3.11.4"},
		{"uptime", "2h 30m"},
		{"rust core", "Rust Core: ✅"},
		{"sessions", "Sessions: 1"},
		{"ws", "WS: 0"},
		{"usage header", "API 사용량"},
		{"provider name", "anthropic"},
		{"calls", "142회"},
		{"channel healthy", "💚"},
		{"channel name", "Telegram"},
		{"channel status", "정상"},
	}
	for _, c := range checks {
		if !strings.Contains(msg, c.fragment) {
			t.Errorf("missing %s: expected %q in:\n%s", c.label, c.fragment, msg)
		}
	}
}

func TestBuildStatusMessage_UnhealthyChannel(t *testing.T) {
	report := StatusReport{
		SessionKey: "telegram:123",
		Channel:    "telegram",
		Version:    "3.11.4",
		StartedAt:  time.Now().Add(-10 * time.Minute),
		ChannelHealth: []ChannelHealthEntry{
			{ID: "Telegram", Healthy: false, Reason: "연결 끊김"},
		},
	}
	msg := BuildStatusMessage(report)
	if !strings.Contains(msg, "❌") {
		t.Error("missing unhealthy icon")
	}
	if !strings.Contains(msg, "연결 끊김") {
		t.Error("missing unhealthy reason")
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0m"},
		{30 * time.Second, "0m"},
		{5 * time.Minute, "5m"},
		{2*time.Hour + 15*time.Minute, "2h 15m"},
		{49*time.Hour + 30*time.Minute, "2d 1h 30m"},
	}
	for _, tt := range tests {
		got := formatUptime(tt.d)
		if got != tt.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestFormatCompactTokens(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{500, "500"},
		{1_500, "1.5K"},
		{1_200_000, "1.2M"},
	}
	for _, tt := range tests {
		got := formatCompactTokens(tt.n)
		if got != tt.want {
			t.Errorf("formatCompactTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
