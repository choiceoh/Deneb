// Internal hook system: two-level event matching, eligibility filtering,
// and programmatic hook registration.
//
// This extends the basic hooks.go with the richer functionality from
// src/hooks/internal-hooks.ts in the TypeScript codebase.
package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// InternalHookEventType categorizes hook events.
type InternalHookEventType string

const (
	EventTypeCommand InternalHookEventType = "command"
	EventTypeSession InternalHookEventType = "session"
	EventTypeAgent   InternalHookEventType = "agent"
	EventTypeGateway InternalHookEventType = "gateway"
	EventTypeMessage InternalHookEventType = "message"
)

// InternalHookEvent is a rich event passed to internal hook handlers.
type InternalHookEvent struct {
	Type       InternalHookEventType `json:"type"`
	Action     string                `json:"action"`
	SessionKey string                `json:"sessionKey"`
	Context    map[string]any        `json:"context,omitempty"`
	Timestamp  time.Time             `json:"timestamp"`
	Messages   []string              `json:"messages,omitempty"`
}

// EventKey returns the fully qualified event key (type:action).
func (e *InternalHookEvent) EventKey() string {
	return string(e.Type) + ":" + e.Action
}

// InternalHookHandler is a function that handles an internal hook event.
type InternalHookHandler func(ctx context.Context, event *InternalHookEvent) error

// DenebHookMetadata is parsed metadata from a HOOK.md frontmatter.
type DenebHookMetadata struct {
	Always   bool              `json:"always,omitempty"`
	HookKey  string            `json:"hookKey,omitempty"`
	Emoji    string            `json:"emoji,omitempty"`
	Homepage string            `json:"homepage,omitempty"`
	Events   []string          `json:"events"`
	Export   string            `json:"export,omitempty"`
	Requires *HookRequires     `json:"requires,omitempty"`
	Install  []HookInstallSpec `json:"install,omitempty"`
}

// HookRequires defines dependency requirements for a hook.
type HookRequires struct {
	Bins    []string `json:"bins,omitempty"`
	AnyBins []string `json:"anyBins,omitempty"`
	Env     []string `json:"env,omitempty"`
	Config  []string `json:"config,omitempty"`
}

// HookInstallSpec describes how to install a hook dependency.
type HookInstallSpec struct {
	ID    string `json:"id,omitempty"`
	Kind  string `json:"kind"`
	Label string `json:"label,omitempty"`
}

// HookEntry is a fully resolved hook with metadata and config.
type HookEntry struct {
	Name     string             `json:"name"`
	Dir      string             `json:"dir"`
	Source   HookSource         `json:"source"`
	Metadata *DenebHookMetadata `json:"metadata,omitempty"`
	Enabled  bool               `json:"enabled"`
}

// HookSource indicates the origin of a hook.
type HookSource string

const (
	HookSourceBundled   HookSource = "bundled"
	HookSourceManaged   HookSource = "managed"
	HookSourceWorkspace HookSource = "workspace"
	HookSourcePlugin    HookSource = "plugin"
	HookSourceExtra     HookSource = "extra"
)

// InternalRegistry manages programmatic (internal) hook handlers with
// two-level event matching: handlers can register for a type ("command")
// or a specific type:action ("command:new").
//
// When an event fires, handlers for the type are called first, then
// handlers for the specific type:action, all in registration order.
type InternalRegistry struct {
	mu       sync.RWMutex
	handlers map[string][]namedHandler // key: "type" or "type:action"
	logger   *slog.Logger
}

type namedHandler struct {
	name    string
	handler InternalHookHandler
}

// NewInternalRegistry creates a new internal hook registry.
func NewInternalRegistry(logger *slog.Logger) *InternalRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	return &InternalRegistry{
		handlers: make(map[string][]namedHandler),
		logger:   logger,
	}
}

// Register adds a handler for an event key.
// eventKey can be a type ("command") or type:action ("command:new").
func (r *InternalRegistry) Register(eventKey, name string, handler InternalHookHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[eventKey] = append(r.handlers[eventKey], namedHandler{name: name, handler: handler})
	r.logger.Debug("internal hook registered", "event", eventKey, "name", name)
}

// Unregister removes a handler by name from an event key.
func (r *InternalRegistry) Unregister(eventKey, name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	handlers, ok := r.handlers[eventKey]
	if !ok {
		return false
	}
	for i, h := range handlers {
		if h.name == name {
			r.handlers[eventKey] = append(handlers[:i], handlers[i+1:]...)
			return true
		}
	}
	return false
}

// Clear removes all handlers (useful for testing).
func (r *InternalRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = make(map[string][]namedHandler)
}

// Trigger fires an event, calling all matching handlers sequentially.
// Type handlers run first, then specific type:action handlers.
// Errors are logged but don't prevent subsequent handlers from running.
func (r *InternalRegistry) Trigger(ctx context.Context, event *InternalHookEvent) {
	r.mu.RLock()
	typeKey := string(event.Type)
	specificKey := event.EventKey()

	// Collect handlers: type first, then specific.
	var all []namedHandler
	all = append(all, r.handlers[typeKey]...)
	if typeKey != specificKey {
		all = append(all, r.handlers[specificKey]...)
	}
	r.mu.RUnlock()

	if len(all) == 0 {
		return
	}

	for _, h := range all {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					r.logger.Error("internal hook panic",
						"event", specificKey,
						"handler", h.name,
						"error", fmt.Sprint(rec),
					)
				}
			}()
			if err := h.handler(ctx, event); err != nil {
				r.logger.Error("internal hook error",
					"event", specificKey,
					"handler", h.name,
					"error", err.Error(),
				)
			}
		}()
	}
}

// TriggerFromEvent bridges the shell hook Event type to internal hook events.
// Converts a shell event + environment map into a structured InternalHookEvent
// and triggers matching internal handlers.
func (r *InternalRegistry) TriggerFromEvent(ctx context.Context, event Event, sessionKey string, env map[string]string) {
	eventType, action := eventToInternal(event)
	eventCtx := make(map[string]any, len(env))
	for k, v := range env {
		eventCtx[k] = v
	}
	r.Trigger(ctx, &InternalHookEvent{
		Type:       eventType,
		Action:     action,
		SessionKey: sessionKey,
		Context:    eventCtx,
		Timestamp:  time.Now(),
	})
}

// eventToInternal maps a shell hook Event to an InternalHookEventType + action.
func eventToInternal(event Event) (InternalHookEventType, string) {
	switch event {
	case EventGatewayStart:
		return EventTypeGateway, "start"
	case EventGatewayStop:
		return EventTypeGateway, "stop"
	case EventSessionStart:
		return EventTypeSession, "start"
	case EventSessionEnd:
		return EventTypeSession, "end"
	case EventMessageReceive:
		return EventTypeMessage, "receive"
	case EventMessageSend:
		return EventTypeMessage, "send"
	case EventChannelConnect:
		return EventTypeGateway, "channel-connect"
	case EventChannelDisconnect:
		return EventTypeGateway, "channel-disconnect"
	case EventToolUse:
		return EventTypeCommand, "tool-use"
	case EventGitHubWebhook:
		return EventTypeGateway, "github-webhook"
	default:
		return InternalHookEventType(event), string(event)
	}
}

// ListHandlers returns the names of all registered handlers per event key.
func (r *InternalRegistry) ListHandlers() map[string][]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string][]string, len(r.handlers))
	for key, handlers := range r.handlers {
		names := make([]string, len(handlers))
		for i, h := range handlers {
			names[i] = h.name
		}
		result[key] = names
	}
	return result
}

// --- Eligibility evaluation ---

// EligibilityContext provides runtime info for evaluating hook eligibility.
type EligibilityContext struct {
	EnvLookup    func(string) string
	BinLookup    func(string) bool
	ConfigLookup func(string) bool
}

// DefaultEligibilityContext creates an eligibility context using the current runtime.
func DefaultEligibilityContext(envLookup func(string) string, configLookup func(string) bool) EligibilityContext {
	return EligibilityContext{
		EnvLookup:    envLookup,
		BinLookup:    hasBinary,
		ConfigLookup: configLookup,
	}
}

// EvaluateEligibility checks if a hook should be loaded based on its requirements.
func EvaluateEligibility(meta *DenebHookMetadata, ectx EligibilityContext) bool {
	if meta == nil {
		return true
	}
	if meta.Always {
		return true
	}

	// Requires check.
	if meta.Requires != nil {
		req := meta.Requires

		// All bins must exist.
		if len(req.Bins) > 0 && ectx.BinLookup != nil {
			for _, bin := range req.Bins {
				if !ectx.BinLookup(bin) {
					return false
				}
			}
		}

		// At least one of anyBins must exist.
		if len(req.AnyBins) > 0 && ectx.BinLookup != nil {
			found := false
			for _, bin := range req.AnyBins {
				if ectx.BinLookup(bin) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}

		// All env vars must be set.
		if len(req.Env) > 0 && ectx.EnvLookup != nil {
			for _, env := range req.Env {
				if ectx.EnvLookup(env) == "" {
					return false
				}
			}
		}

		// All config paths must be truthy.
		if len(req.Config) > 0 && ectx.ConfigLookup != nil {
			for _, path := range req.Config {
				if !ectx.ConfigLookup(path) {
					return false
				}
			}
		}
	}

	return true
}

// hasBinary checks if a binary is available in PATH.
func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
