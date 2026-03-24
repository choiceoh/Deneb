package rpc

import "github.com/choiceoh/deneb/gateway-go/internal/auth"

// methodScopes maps each registered RPC method to the minimum scope required.
// Methods not listed here are assumed to require admin scope.
// Public methods (no auth required) are listed in publicMethods.
//
// This mirrors the scope system in src/gateway/method-scopes.ts.
var methodScopes = map[string]auth.Scope{
	// --- Health & Status (read) ---
	"health":       auth.ScopeRead,
	"health.check": auth.ScopeRead,
	"status":       auth.ScopeRead,
	"system.info":  auth.ScopeRead,

	// --- Sessions (read/write) ---
	"sessions.list":      auth.ScopeRead,
	"sessions.get":       auth.ScopeRead,
	"sessions.delete":    auth.ScopeWrite,
	"sessions.create":    auth.ScopeWrite,
	"sessions.lifecycle": auth.ScopeWrite,

	// --- Channels (read/write/admin) ---
	"channels.list":    auth.ScopeRead,
	"channels.get":     auth.ScopeRead,
	"channels.status":  auth.ScopeRead,
	"channels.health":  auth.ScopeRead,
	"channels.start":   auth.ScopeAdmin,
	"channels.stop":    auth.ScopeAdmin,
	"channels.restart": auth.ScopeAdmin,

	// --- Agent (read/write) ---
	"agent.status": auth.ScopeRead,

	// --- Process (write) ---
	"process.exec": auth.ScopeApprovals,
	"process.kill": auth.ScopeWrite,
	"process.get":  auth.ScopeRead,
	"process.list": auth.ScopeRead,

	// --- Cron (read/write) ---
	"cron.list":       auth.ScopeRead,
	"cron.get":        auth.ScopeRead,
	"cron.unregister": auth.ScopeWrite,

	// --- Hooks (read/admin) ---
	"hooks.list":       auth.ScopeRead,
	"hooks.register":   auth.ScopeAdmin,
	"hooks.unregister": auth.ScopeAdmin,
	"hooks.fire":       auth.ScopeWrite,

	// --- Chat (write) ---
	"chat.send":    auth.ScopeWrite,
	"chat.history": auth.ScopeRead,
	"chat.abort":   auth.ScopeWrite,
	"chat.inject":  auth.ScopeWrite,

	// --- Monitoring (read) ---
	"monitoring.channel_health": auth.ScopeRead,
	"monitoring.activity":       auth.ScopeRead,

	// --- Event subscriptions (read) ---
	"node.event":                    auth.ScopeWrite,
	"subscribe.session":             auth.ScopeRead,
	"unsubscribe.session":           auth.ScopeRead,
	"subscribe.session.messages":    auth.ScopeRead,
	"unsubscribe.session.messages":  auth.ScopeRead,

	// --- Security & Media (read) ---
	"protocol.validate":            auth.ScopeRead,
	"security.validate_session_key": auth.ScopeRead,
	"security.sanitize_html":        auth.ScopeRead,
	"security.is_safe_url":          auth.ScopeRead,
	"security.validate_error_code":  auth.ScopeRead,
	"media.detect_mime":             auth.ScopeRead,

	// --- Providers (read) ---
	"providers.list":    auth.ScopeRead,
	"providers.catalog": auth.ScopeRead,

	// --- Config (admin) ---
	"config.get":    auth.ScopeAdmin,
	"config.reload": auth.ScopeAdmin,

	// --- Daemon (admin) ---
	"daemon.status": auth.ScopeAdmin,

	// --- Events (admin) ---
	"events.broadcast": auth.ScopeAdmin,
}

// RequiredScope returns the scope needed to call the given method.
// Returns ScopeAdmin for unknown methods (fail-closed).
func RequiredScope(method string) auth.Scope {
	if scope, ok := methodScopes[method]; ok {
		return scope
	}
	return auth.ScopeAdmin
}
