// acp_translator.go — Bidirectional translation between ACP protocol and
// Gateway protocol. Handles prompt conversion, session resolution, lifecycle
// event mapping, and stop-reason translation.
//
// Mirrors the ACP bridge translation logic documented in docs/docs.acp.md.
package acp

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
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

// ResolveGatewaySessionKey resolves an ACP session key to the target gateway
// session key. Checks metadata overrides first, then uses the key as-is.
func (t *ACPTranslator) ResolveGatewaySessionKey(input ACPPromptInput) string {
	// Check for explicit sessionKey override in metadata.
	if sk, ok := input.Meta["sessionKey"]; ok && sk != "" {
		return sk
	}
	// Check for sessionLabel override — resolve by label.
	if label, ok := input.Meta["sessionLabel"]; ok && label != "" {
		if agent := t.findAgentByLabel(label); agent != nil {
			return agent.SessionKey
		}
	}
	return input.SessionKey
}

// TranslatePromptToChat converts an ACP prompt input to a Gateway dispatch
// configuration and flattened prompt string.
func (t *ACPTranslator) TranslatePromptToChat(input ACPPromptInput) (*ACPDispatchConfig, string) {
	gatewayKey := t.ResolveGatewaySessionKey(input)

	// Build the prompt text by flattening text + resources.
	prompt := t.flattenPrompt(input)

	cfg := &ACPDispatchConfig{
		SessionKey: gatewayKey,
		Channel:    "acp",
	}

	return cfg, prompt
}

// TranslateLifecycleToACPEvent maps a gateway lifecycle event to an ACP event.
// Returns nil for non-ACP sessions or unrecognized events.
func (t *ACPTranslator) TranslateLifecycleToACPEvent(evt events.LifecycleChangeEvent) *ACPEventOutput {
	if !IsACPSession(evt.SessionKey) {
		return nil
	}

	kind := ""
	stopReason := ""

	switch evt.Reason {
	case "start":
		kind = "running"
	case "end":
		kind = "done"
		stopReason = "stop"
	case "error":
		kind = "error"
		stopReason = "error"
	case "deleted":
		kind = "done"
		stopReason = "cancel"
	default:
		// Check if the reason maps to a known status.
		if s := mapReasonToStopReason(evt.Reason); s != "" {
			kind = "done"
			stopReason = s
		} else {
			return nil
		}
	}

	return &ACPEventOutput{
		Kind:       kind,
		SessionKey: evt.SessionKey,
		StopReason: stopReason,
	}
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

// flattenPrompt combines text and resources into a single prompt string.
func (t *ACPTranslator) flattenPrompt(input ACPPromptInput) string {
	var parts []string

	// Prefix working directory if set.
	if input.WorkingDir != "" {
		parts = append(parts, fmt.Sprintf("[cwd: %s]", input.WorkingDir))
	}

	// Primary text.
	if input.Text != "" {
		parts = append(parts, input.Text)
	}

	// Flatten resources into text.
	for _, r := range input.Resources {
		switch r.Kind {
		case "text":
			if r.Content != "" {
				if r.Name != "" {
					parts = append(parts, fmt.Sprintf("--- %s ---\n%s", r.Name, r.Content))
				} else {
					parts = append(parts, r.Content)
				}
			}
		case "resource_link":
			if r.Content != "" {
				label := r.Name
				if label == "" {
					label = "resource"
				}
				parts = append(parts, fmt.Sprintf("[%s: %s]", label, r.Content))
			}
		case "image":
			if r.Content != "" {
				parts = append(parts, fmt.Sprintf("[image: %s]", r.Content))
			}
		}
	}

	return strings.Join(parts, "\n\n")
}

// findAgentByLabel finds an agent by its role (used as label).
func (t *ACPTranslator) findAgentByLabel(label string) *ACPAgent {
	if t.registry == nil {
		return nil
	}
	agents := t.registry.List("")
	for _, a := range agents {
		if a.Role == label || a.ID == label {
			return &a
		}
	}
	return nil
}

// mapReasonToStopReason maps lifecycle event reasons to ACP stop reasons.
func mapReasonToStopReason(reason string) string {
	switch reason {
	case "aborted":
		return "cancel"
	case "completed", "done":
		return "stop"
	case "failed", "timeout":
		return "error"
	default:
		return ""
	}
}

// BuildACPDispatch creates an ACPDispatch configuration for an ACP session.
func BuildACPDispatch(sessionKey string, mode string) ACPDispatch {
	if mode == "" {
		mode = "stream"
	}
	return ACPDispatch{
		Enabled:     true,
		TargetAgent: sessionKey,
		Mode:        mode,
	}
}

// BuildACPDelivery creates an ACPDelivery for a completed agent turn.
func BuildACPDelivery(sessionKey, agentID string, result *ACPTurnResult) ACPDelivery {
	return ACPDelivery{
		SessionKey: sessionKey,
		AgentID:    agentID,
		Result:     result,
	}
}
