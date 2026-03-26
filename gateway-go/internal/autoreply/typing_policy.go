package autoreply

// InternalMessageChannel is the well-known channel name for internal webchat messages.
// Matches INTERNAL_MESSAGE_CHANNEL from src/utils/message-channel.ts.
const InternalMessageChannel = "webchat"

// ResolveRunTypingPolicyParams holds the inputs for typing policy resolution.
type ResolveRunTypingPolicyParams struct {
	RequestedPolicy    TypingPolicy
	SuppressTyping     bool
	IsHeartbeat        bool
	OriginatingChannel string
	SystemEvent        bool
}

// ResolvedRunTypingPolicy holds the resolved typing policy and suppression flag.
type ResolvedRunTypingPolicy struct {
	TypingPolicy   TypingPolicy
	SuppressTyping bool
}

// ResolveRunTypingPolicy resolves the typing policy and suppression flag
// based on context (heartbeat, internal webchat, system event).
//
// Mirrors src/auto-reply/reply/typing-policy.ts.
func ResolveRunTypingPolicy(params ResolveRunTypingPolicyParams) ResolvedRunTypingPolicy {
	var policy TypingPolicy
	switch {
	case params.IsHeartbeat:
		policy = TypingPolicyHeartbeat
	case params.OriginatingChannel == InternalMessageChannel:
		policy = TypingPolicyInternalWeb
	case params.SystemEvent:
		policy = TypingPolicySystemEvent
	default:
		if params.RequestedPolicy != "" {
			policy = params.RequestedPolicy
		} else {
			policy = "auto"
		}
	}

	suppress := params.SuppressTyping ||
		policy == TypingPolicyHeartbeat ||
		policy == TypingPolicySystemEvent ||
		policy == TypingPolicyInternalWeb

	return ResolvedRunTypingPolicy{
		TypingPolicy:   policy,
		SuppressTyping: suppress,
	}
}
