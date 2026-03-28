package typing

import (
	"testing"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

func TestResolveRunTypingPolicy(t *testing.T) {
	tests := []struct {
		name         string
		params       ResolveRunTypingPolicyParams
		wantPolicy   types.TypingPolicy
		wantSuppress bool
	}{
		{
			name:         "heartbeat forces heartbeat policy and suppresses",
			params:       ResolveRunTypingPolicyParams{IsHeartbeat: true},
			wantPolicy:   types.TypingPolicyHeartbeat,
			wantSuppress: true,
		},
		{
			name:         "internal webchat forces internal_webchat policy and suppresses",
			params:       ResolveRunTypingPolicyParams{OriginatingChannel: "webchat"},
			wantPolicy:   types.TypingPolicyInternalWeb,
			wantSuppress: true,
		},
		{
			name:         "system event forces system_event policy and suppresses",
			params:       ResolveRunTypingPolicyParams{SystemEvent: true},
			wantPolicy:   types.TypingPolicySystemEvent,
			wantSuppress: true,
		},
		{
			name:         "explicit requested policy is used",
			params:       ResolveRunTypingPolicyParams{RequestedPolicy: "user_message"},
			wantPolicy:   "user_message",
			wantSuppress: false,
		},
		{
			name:         "no requested policy defaults to auto",
			params:       ResolveRunTypingPolicyParams{},
			wantPolicy:   "auto",
			wantSuppress: false,
		},
		{
			name:         "explicit suppress overrides normal policy",
			params:       ResolveRunTypingPolicyParams{SuppressTyping: true, RequestedPolicy: "user_message"},
			wantPolicy:   "user_message",
			wantSuppress: true,
		},
		{
			name:         "heartbeat takes priority over system event",
			params:       ResolveRunTypingPolicyParams{IsHeartbeat: true, SystemEvent: true},
			wantPolicy:   types.TypingPolicyHeartbeat,
			wantSuppress: true,
		},
		{
			name:         "internal webchat takes priority over system event",
			params:       ResolveRunTypingPolicyParams{OriginatingChannel: "webchat", SystemEvent: true},
			wantPolicy:   types.TypingPolicyInternalWeb,
			wantSuppress: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ResolveRunTypingPolicy(tt.params)
			if result.TypingPolicy != tt.wantPolicy {
				t.Errorf("policy = %q, want %q", result.TypingPolicy, tt.wantPolicy)
			}
			if result.SuppressTyping != tt.wantSuppress {
				t.Errorf("suppress = %v, want %v", result.SuppressTyping, tt.wantSuppress)
			}
		})
	}
}
