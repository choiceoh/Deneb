// context_injection.go — Injects subagent results into the parent session's
// transcript so the next LLM turn has context about what subagents produced.
package acp

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// TranscriptAppender appends a system note to a session transcript.
type TranscriptAppender interface {
	AppendSystemNote(sessionKey, text string) error
}

// TranscriptAppendFunc adapts a plain function to the TranscriptAppender interface.
type TranscriptAppendFunc func(sessionKey, text string) error

func (f TranscriptAppendFunc) AppendSystemNote(sessionKey, text string) error {
	return f(sessionKey, text)
}

// ResultInjectionDeps groups the dependencies for subagent result injection.
type ResultInjectionDeps struct {
	Registry   *ACPRegistry
	Projector  *ACPProjector
	Sessions   *session.Manager
	Transcript TranscriptAppender
	Logger     *slog.Logger
}

// StartSubagentResultInjection subscribes to session lifecycle events and
// injects completed subagent outputs into the parent session's transcript.
// This ensures the parent agent's next turn sees what its subagents produced.
// Returns an unsubscribe function.
func StartSubagentResultInjection(deps ResultInjectionDeps) func() {
	if deps.Registry == nil || deps.Sessions == nil || deps.Transcript == nil {
		return func() {}
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return deps.Sessions.EventBusRef().Subscribe(func(event session.Event) {
		if event.Kind != session.EventStatusChanged {
			return
		}
		if event.NewStatus != session.StatusDone {
			return
		}

		// Only handle subagent sessions (key format: "acp:{parentKey}:{agentID}").
		parentKey, agentID := parseACPSessionKey(event.Key)
		if parentKey == "" {
			return
		}

		// Get the subagent's last output from the session.
		sess := deps.Sessions.Get(event.Key)
		if sess == nil || sess.LastOutput == "" {
			return
		}

		// Format the result using ACPProjector if available.
		text := sess.LastOutput
		if deps.Projector != nil {
			result := &ACPTurnResult{OutputText: text}
			formatted := deps.Projector.ProjectResult(agentID, result)
			if formatted != "" {
				text = formatted
			}
		}

		// Truncate very long outputs to avoid bloating the parent transcript.
		const maxLen = 4000
		if len(text) > maxLen {
			text = text[:maxLen] + "\n... (truncated)"
		}

		note := fmt.Sprintf("[Subagent completed] %s", text)
		if err := deps.Transcript.AppendSystemNote(parentKey, note); err != nil {
			logger.Warn("failed to inject subagent result into parent transcript",
				"parentSession", parentKey,
				"subagentSession", event.Key,
				"error", err,
			)
		} else {
			logger.Info("injected subagent result into parent transcript",
				"parentSession", parentKey,
				"agentId", agentID,
				"outputLen", len(text),
			)
		}
	})
}

// parseACPSessionKey extracts the parent session key and agent ID from an ACP
// session key. Returns ("", "") if the key is not an ACP subagent key.
// Format: "acp:{parentSessionKey}:{agentID}"
func parseACPSessionKey(key string) (parentKey, agentID string) {
	if !strings.HasPrefix(key, "acp:") {
		return "", ""
	}
	rest := key[len("acp:"):]
	// Find the last colon — everything before it is the parent key,
	// everything after is the agent ID.
	idx := strings.LastIndex(rest, ":")
	if idx <= 0 {
		return "", ""
	}
	return rest[:idx], rest[idx+1:]
}
