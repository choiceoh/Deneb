package typing

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
)

// ResolveRunTypingPolicyParams holds the inputs for typing policy resolution.
type ResolveRunTypingPolicyParams struct {
	RequestedPolicy types.TypingPolicy
	SuppressTyping  bool
	IsHeartbeat     bool
	SystemEvent     bool
}

// ResolvedRunTypingPolicy holds the resolved typing policy and suppression flag.
type ResolvedRunTypingPolicy struct {
	TypingPolicy   types.TypingPolicy
	SuppressTyping bool
}

// ResolveRunTypingPolicy resolves the typing policy and suppression flag
// based on context (heartbeat, system event).
//
// Mirrors src/auto-reply/reply/typing-policy.ts.
func ResolveRunTypingPolicy(params ResolveRunTypingPolicyParams) ResolvedRunTypingPolicy {
	var policy types.TypingPolicy
	switch {
	case params.IsHeartbeat:
		policy = types.TypingPolicyHeartbeat
	case params.SystemEvent:
		policy = types.TypingPolicySystemEvent
	default:
		if params.RequestedPolicy != "" {
			policy = params.RequestedPolicy
		} else {
			policy = "auto"
		}
	}

	suppress := params.SuppressTyping ||
		policy == types.TypingPolicyHeartbeat ||
		policy == types.TypingPolicySystemEvent

	return ResolvedRunTypingPolicy{
		TypingPolicy:   policy,
		SuppressTyping: suppress,
	}
}
