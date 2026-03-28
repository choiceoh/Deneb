package autoreply

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"
)

func TestResolveRunTypingPolicy(t *testing.T) {
	tests := []struct {
		name         string
		params       typing.ResolveRunTypingPolicyParams
		wantPolicy   types.TypingPolicy
		wantSuppress bool
	}{
		{
			name:         "heartbeat forces heartbeat policy and suppresses",
			params:       typing.ResolveRunTypingPolicyParams{IsHeartbeat: true},
			wantPolicy:   types.TypingPolicyHeartbeat,
			wantSuppress: true,
		},
		{
			name:         "internal webchat forces internal_webchat policy and suppresses",
			params:       typing.ResolveRunTypingPolicyParams{OriginatingChannel: "webchat"},
			wantPolicy:   types.TypingPolicyInternalWeb,
			wantSuppress: true,
		},
		{
			name:         "system event forces system_event policy and suppresses",
			params:       typing.ResolveRunTypingPolicyParams{SystemEvent: true},
			wantPolicy:   types.TypingPolicySystemEvent,
			wantSuppress: true,
		},
		{
			name:         "explicit requested policy is used",
			params:       typing.ResolveRunTypingPolicyParams{RequestedPolicy: "user_message"},
			wantPolicy:   "user_message",
			wantSuppress: false,
		},
		{
			name:         "no requested policy defaults to auto",
			params:       typing.ResolveRunTypingPolicyParams{},
			wantPolicy:   "auto",
			wantSuppress: false,
		},
		{
			name:         "explicit suppress overrides normal policy",
			params:       typing.ResolveRunTypingPolicyParams{SuppressTyping: true, RequestedPolicy: "user_message"},
			wantPolicy:   "user_message",
			wantSuppress: true,
		},
		{
			name:         "heartbeat takes priority over system event",
			params:       typing.ResolveRunTypingPolicyParams{IsHeartbeat: true, SystemEvent: true},
			wantPolicy:   types.TypingPolicyHeartbeat,
			wantSuppress: true,
		},
		{
			name:         "internal webchat takes priority over system event",
			params:       typing.ResolveRunTypingPolicyParams{OriginatingChannel: "webchat", SystemEvent: true},
			wantPolicy:   types.TypingPolicyInternalWeb,
			wantSuppress: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := typing.ResolveRunTypingPolicy(tt.params)
			if result.TypingPolicy != tt.wantPolicy {
				t.Errorf("policy = %q, want %q", result.TypingPolicy, tt.wantPolicy)
			}
			if result.SuppressTyping != tt.wantSuppress {
				t.Errorf("suppress = %v, want %v", result.SuppressTyping, tt.wantSuppress)
			}
		})
	}
}
