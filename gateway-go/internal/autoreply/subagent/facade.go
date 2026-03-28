package subagent

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// Re-export key ACP-facing types under subagent package.
type (
	ACPPromptInput    = acp.ACPPromptInput
	ACPResource       = acp.ACPResource
	ACPDispatchConfig = acp.ACPDispatchConfig
	ACPEventOutput    = acp.ACPEventOutput
	ACPTranslator     = acp.ACPTranslator
)

const ACPSessionPrefix = acp.ACPSessionPrefix

// NewACPTranslator creates a new ACP translator.
func NewACPTranslator(registry *acp.ACPRegistry, bindings *acp.SessionBindingService) *ACPTranslator {
	return acp.NewACPTranslator(registry, bindings)
}

// IsACPSession returns true if the session key is ACP-originated.
func IsACPSession(sessionKey string) bool {
	return acp.IsACPSession(sessionKey)
}

// TranslateStopReason maps run status to ACP stop reason.
func TranslateStopReason(status session.RunStatus) string {
	return acp.TranslateStopReason(status)
}

// TranslateACPStopReasonToStatus maps ACP stop reason to run status.
func TranslateACPStopReasonToStatus(stopReason string) session.RunStatus {
	return acp.TranslateACPStopReasonToStatus(stopReason)
}
