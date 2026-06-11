// acp_translator.go — Bidirectional translation between ACP protocol and
// Gateway protocol. Handles prompt conversion, session resolution, lifecycle
// event mapping, and stop-reason translation.
//
// Mirrors the ACP bridge translation logic documented in docs/docs.acp.md.
package acp

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// ACPSessionPrefix is the prefix for ACP-originated session keys.
const ACPSessionPrefix = "acp:"

// ACPPromptInput represents an inbound ACP prompt to be translated.
type ACPPromptInput struct {
	SessionKey string            // ACP session key (e.g. "acp:<uuid>")
	Text       string            // Primary prompt text
	Resources  []ACPResource     // Attached resources (files, images)
	Meta       map[string]string // Optional metadata (_meta fields)
	WorkingDir string            // Client working directory (prefixed if set)
}

// ACPResource represents an attached resource in an ACP prompt.
type ACPResource struct {
	Kind     string // "text", "resource_link", "image"
	Content  string // Text content or URI
	MimeType string // MIME type for images/resources
	Name     string // Display name
}

// ACPEventOutput represents a translated ACP event for sending to clients.
type ACPEventOutput struct {
	Kind       string // "running", "done", "cancel", "error"
	SessionKey string
	StopReason string // ACP stop reason: "stop", "cancel", "error"
	Message    string // Optional status message
}

// ACPDispatchConfig holds the minimal dispatch configuration derived from an
// ACP prompt. It is a self-contained type that does not depend on the full
// autoreply.DispatchConfig; callers map it as needed.
type ACPDispatchConfig struct {
	SessionKey string
	Channel    string
}

// ACPDispatch handles ACP (Agent Control Protocol) routing.
type ACPDispatch struct {
	Enabled     bool
	TargetAgent string
	Mode        string // "stream" or "batch"
}

// ACPDelivery handles delivering ACP results.
type ACPDelivery struct {
	SessionKey string
	AgentID    string
	Result     *ACPTurnResult
}

// ACPTranslator handles bidirectional ACP ↔ Gateway translation.
type ACPTranslator struct {
	registry *ACPRegistry
	bindings *SessionBindingService
}

// NewACPTranslator creates a new ACP translator.
func NewACPTranslator(registry *ACPRegistry, bindings *SessionBindingService) *ACPTranslator {
	return &ACPTranslator{
		registry: registry,
		bindings: bindings,
	}
}

// IsACPSession returns true if the session key is an ACP-originated session.
func IsACPSession(sessionKey string) bool {
	return strings.HasPrefix(sessionKey, ACPSessionPrefix)
}

// TranslateStopReason maps a session RunStatus to an ACP stop reason string.
// Per docs: done→"stop", killed→"cancel", failed/timeout→"error".
func TranslateStopReason(status session.RunStatus) string {
	switch status {
	case session.StatusDone:
		return "stop"
	case session.StatusKilled:
		return "cancel"
	case session.StatusFailed, session.StatusTimeout:
		return "error"
	default:
		return ""
	}
}

// TranslateACPStopReasonToStatus maps an ACP stop reason back to a session RunStatus.
func TranslateACPStopReasonToStatus(stopReason string) session.RunStatus {
	switch stopReason {
	case "stop":
		return session.StatusDone
	case "cancel":
		return session.StatusKilled
	case "error":
		return session.StatusFailed
	default:
		return session.StatusDone
	}
}
