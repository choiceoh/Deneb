// Package bridge provides the bridge.send RPC handler for inter-agent
// communication. It broadcasts lightweight messages to WebSocket clients
// (including the MCP server) without triggering LLM inference.
//
// When an Injector is set (late-bound after chat handler creation), incoming
// bridge messages are also injected into the active shadow session so the
// main agent can see them in its conversation context.
package bridge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// InjectFunc injects a message into a session transcript.
// Signature matches chat.Handler.InjectDirect (sessionKey, role, content).
type InjectFunc func(sessionKey, role, content string) error

// SessionLister returns active session keys matching a predicate.
type SessionLister func() []string

// Injector handles late-bound injection of bridge messages into the active
// session. Created empty during early registration, populated after chat
// handler is ready.
type Injector struct {
	mu           sync.RWMutex
	injectFn     InjectFunc
	sessionsList SessionLister
}

// SetInject configures the injection function and session lister.
// Called from registerLateMethods after chat handler is created.
func (inj *Injector) SetInject(fn InjectFunc, sessions SessionLister) {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	inj.injectFn = fn
	inj.sessionsList = sessions
}

// inject sends a bridge message into the active shadow session.
// No-op if inject function is not yet set.
func (inj *Injector) inject(source, message string) {
	inj.mu.RLock()
	fn := inj.injectFn
	lister := inj.sessionsList
	inj.mu.RUnlock()

	if fn == nil || lister == nil {
		return
	}

	keys := lister()
	if len(keys) == 0 {
		return
	}

	content := fmt.Sprintf("[bridge:%s] %s", source, message)
	for _, key := range keys {
		// Use "user" role so all LLM models see it in the conversation flow.
		// The [bridge:SOURCE] tag distinguishes it from real user messages.
		_ = fn(key, "user", content)
	}
}

// Deps holds dependencies for bridge RPC handlers.
type Deps struct {
	Broadcaster rpcutil.BroadcastFunc
	Injector    *Injector // late-bound; nil-safe
}

// Methods returns the bridge RPC handlers.
// Returns nil if Broadcaster is not configured.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Broadcaster == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"bridge.send": bridgeSend(deps),
	}
}

// bridgeSend broadcasts a bridge.message event to all WebSocket clients.
// If an injector is configured, also injects the message into the active
// shadow session for the main agent to see.
func bridgeSend(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Message string `json:"message"`
			Source  string `json:"source,omitempty"` // sender identity (e.g., "main-agent", "claude-code")
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Message == "" {
			return rpcerr.MissingParam("message").Response(req.ID)
		}
		if p.Source == "" {
			p.Source = "gateway"
		}

		ts := time.Now().UnixMilli()
		payload := map[string]any{
			"message": p.Message,
			"source":  p.Source,
			"ts":      ts,
		}

		sent, _ := deps.Broadcaster("bridge.message", payload)

		// Inject into active shadow session so main agent sees the message.
		injected := false
		if deps.Injector != nil && !isFromMainAgent(p.Source) {
			deps.Injector.inject(p.Source, p.Message)
			injected = true
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"sent":     sent,
			"injected": injected,
			"ts":       ts,
		})
	}
}

// isFromMainAgent returns true if the source is the gateway/main agent itself.
// We don't re-inject messages that originated from the main agent to avoid loops.
func isFromMainAgent(source string) bool {
	return source == "gateway" || source == "main-agent" || strings.HasPrefix(source, "deneb")
}
