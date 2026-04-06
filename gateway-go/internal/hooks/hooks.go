// Package hooks provides hook event types and an internal hook registry for
// the gateway. User-defined shell hooks (Registry) have been removed; only
// InternalRegistry (programmatic hooks) remains.
package hooks

// Event represents a hook trigger point.
type Event string

const (
	EventSessionStart      Event = "session.start"
	EventSessionEnd        Event = "session.end"
	EventMessageReceive    Event = "message.receive"
	EventMessageSend       Event = "message.send"
	EventChannelConnect    Event = "channel.connect"
	EventChannelDisconnect Event = "channel.disconnect"
	EventGatewayStart      Event = "gateway.start"
	EventGatewayStop       Event = "gateway.stop"
	EventToolUse           Event = "tool.use"
	EventGitHubWebhook     Event = "github.webhook"
)

// Hook defines a user-configured hook.
type Hook struct {
	ID      string `json:"id"`
	Event   Event  `json:"event"`
	Command string `json:"command"`
	// TimeoutMs is the max time the hook can run (default 30000).
	TimeoutMs int64 `json:"timeoutMs,omitempty"`
	// Blocking determines if the hook must complete before the event proceeds.
	Blocking bool `json:"blocking,omitempty"`
	// Enabled controls whether the hook is active.
	Enabled bool `json:"enabled"`
}

// HookResult is the outcome of a hook execution.
type HookResult struct {
	HookID   string `json:"hookId"`
	Event    Event  `json:"event"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationMs"`
}
