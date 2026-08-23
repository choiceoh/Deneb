package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
	"github.com/choiceoh/deneb/gateway-go/pkg/promptguard"
)

const trustedMemoryInductionSessionKey = "client:main"

// trustedMemoryInductionRun identifies the sole implicit path allowed to
// classify a raw user message into a durable current fact: a real interactive
// turn in the native home session. Model-facing tools cannot mutate the fact
// plane. Session sends, captures, subagent notifications, automations, and
// child chats either lack the server-owned provenance marker or use another
// session key. GateUntrustedTools remains a separate defense and is not itself
// evidence of direct-user provenance.
func trustedMemoryInductionRun(params RunParams) bool {
	return params.SessionKey == trustedMemoryInductionSessionKey &&
		params.TrustedDirectUserInput &&
		params.GateUntrustedTools &&
		!params.EphemeralUser
}

func hasDirectMemoryMutationIntent(message string) bool {
	induced := memory.InduceFromTurn(message)
	return induced != nil && induced.Route == memory.RouteMemory
}

func steerRequiresOwnTurn(message string) bool {
	return hasDirectMemoryMutationIntent(message) || promptguard.HasThreat(message)
}
