package typing

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// InternalMessageChannel is the well-known channel name for internal webchat messages.
// Matches INTERNAL_MESSAGE_CHANNEL from src/utils/message-channel.ts.
const InternalMessageChannel = "webchat"

// ResolveRunTypingPolicyParams holds the inputs for typing policy resolution.
type ResolveRunTypingPolicyParams struct {
	RequestedPolicy    types.TypingPolicy
	SuppressTyping     bool
	IsHeartbeat        bool
	OriginatingChannel string
	SystemEvent        bool
}

// ResolvedRunTypingPolicy holds the resolved typing policy and suppression flag.
type ResolvedRunTypingPolicy struct {
	TypingPolicy   types.TypingPolicy
	SuppressTyping bool
}

// ResolveRunTypingPolicy resolves the typing policy and suppression flag
// based on context (heartbeat, internal webchat, system event).
//
// Mirrors src/auto-reply/reply/typing-policy.ts.
func ResolveRunTypingPolicy(params ResolveRunTypingPolicyParams) ResolvedRunTypingPolicy {
	var policy types.TypingPolicy
	switch {
	case params.IsHeartbeat:
		policy = types.TypingPolicyHeartbeat
	case params.OriginatingChannel == InternalMessageChannel:
		policy = types.TypingPolicyInternalWeb
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
		policy == types.TypingPolicySystemEvent ||
		policy == types.TypingPolicyInternalWeb

	return ResolvedRunTypingPolicy{
		TypingPolicy:   policy,
		SuppressTyping: suppress,
	}
}
