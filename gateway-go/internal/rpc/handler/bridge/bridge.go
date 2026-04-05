// Package bridge provides the bridge.send RPC handler for inter-agent
// communication. It broadcasts lightweight messages to WebSocket clients
// (including the MCP server) and triggers an LLM run on the main agent's
// active session so it can respond immediately.
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

// SendFunc sends a message to a session and triggers an LLM run.
// Signature matches chat.Handler.SendDirect.
type SendFunc func(sessionKey, message string)

// SessionLister returns active session keys matching a predicate.
type SessionLister func() []string

// Injector handles late-bound delivery of bridge messages to the active
// session. Created empty during early registration, populated after chat
// handler is ready.
type Injector struct {
	mu           sync.RWMutex
	sendFn       SendFunc
	sessionsList SessionLister
}

// SetSend configures the send function and session lister.
// Called from registerLateMethods after chat handler is created.
func (inj *Injector) SetSend(fn SendFunc, sessions SessionLister) {
	inj.mu.Lock()
	defer inj.mu.Unlock()
	inj.sendFn = fn
	inj.sessionsList = sessions
}

// send delivers a bridge message to the active session and triggers an LLM run.
// No-op if send function is not yet set.
func (inj *Injector) send(source, message string) {
	inj.mu.RLock()
	fn := inj.sendFn
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
		fn(key, content)
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

// bridgeSend broadcasts a bridge.message event to all WebSocket clients
// and triggers an LLM run on the main agent's active session.
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

		// Send to active session and trigger LLM run so main agent responds.
		triggered := false
		if deps.Injector != nil && !isFromMainAgent(p.Source) {
			deps.Injector.send(p.Source, p.Message)
			triggered = true
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"sent":      sent,
			"triggered": triggered,
			"ts":        ts,
		})
	}
}

// isFromMainAgent returns true if the source is the gateway/main agent itself.
// We don't re-inject messages that originated from the main agent to avoid loops.
func isFromMainAgent(source string) bool {
	return source == "gateway" || source == "main-agent" || strings.HasPrefix(source, "deneb")
}
