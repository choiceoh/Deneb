package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
)

func TestShouldSilenceForChannel(t *testing.T) {
	tests := []struct {
		name       string
		channel    string
		activities []agent.ToolActivity
		want       bool
	}{
		{
			name:    "cron on telegram is NOT silenced (user-initiated queries must respond)",
			channel: "telegram",
			activities: []agent.ToolActivity{
				{Name: "read"},
				{Name: "cron"},
			},
			want: false,
		},
		{
			name:    "cron on api is not silenced",
			channel: "api",
			activities: []agent.ToolActivity{
				{Name: "cron"},
			},
			want: false,
		},
		{
			name:    "non-silent tools on telegram are fine",
			channel: "telegram",
			activities: []agent.ToolActivity{
				{Name: "read"},
				{Name: "exec"},
			},
			want: false,
		},
		{
			name:    "internal tools on telegram are still silenced",
			channel: "telegram",
			activities: []agent.ToolActivity{
				{Name: "sessions"},
			},
			want: true,
		},
		{
			name:    "gmail on telegram is NOT silenced",
			channel: "telegram",
			activities: []agent.ToolActivity{
				{Name: "gmail"},
			},
			want: false,
		},
		{
			name:    "health_check on telegram is NOT silenced",
			channel: "telegram",
			activities: []agent.ToolActivity{
				{Name: "health_check"},
			},
			want: false,
		},
		{
			name:       "empty activities",
			channel:    "telegram",
			activities: nil,
			want:       false,
		},
		{
			name:    "empty channel",
			channel: "",
			activities: []agent.ToolActivity{
				{Name: "cron"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSilenceForChannel(tt.channel, tt.activities); got != tt.want {
				t.Errorf("shouldSilenceForChannel(%q, ...) = %v, want %v", tt.channel, got, tt.want)
			}
		})
	}
}
