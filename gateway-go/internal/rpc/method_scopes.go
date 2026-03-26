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

	// --- System / Diagnostics ---
	"gateway.identity.get": auth.ScopeRead,
	"last-heartbeat":       auth.ScopeRead,
	"set-heartbeats":       auth.ScopeWrite,
	"system-presence":      auth.ScopeAdmin,
	"system-event":         auth.ScopeAdmin,
	"logs.tail":            auth.ScopeRead,
	"doctor.memory.status": auth.ScopeRead,
	"models.list":          auth.ScopeRead,
	"update.run":           auth.ScopeAdmin,
	"maintenance.run":      auth.ScopeAdmin,
	"maintenance.status":   auth.ScopeRead,
	"maintenance.summary":  auth.ScopeRead,

	// --- Sessions (read/write) ---
	"sessions.list":                 auth.ScopeRead,
	"sessions.get":                  auth.ScopeRead,
	"sessions.preview":              auth.ScopeRead,
	"sessions.resolve":              auth.ScopeRead,
	"sessions.subscribe":            auth.ScopeRead,
	"sessions.unsubscribe":          auth.ScopeRead,
	"sessions.messages.subscribe":   auth.ScopeRead,
	"sessions.messages.unsubscribe": auth.ScopeRead,
	"sessions.create":               auth.ScopeWrite,
	"sessions.send":                 auth.ScopeWrite,
	"sessions.steer":                auth.ScopeWrite,
	"sessions.abort":                auth.ScopeWrite,
	"sessions.patch":                auth.ScopeWrite,
	"sessions.reset":                auth.ScopeWrite,
	"sessions.delete":               auth.ScopeWrite,
	"sessions.compact":              auth.ScopeWrite,
	"sessions.repair":               auth.ScopeWrite,
	"sessions.overflow_check":       auth.ScopeRead,
	"sessions.lifecycle":            auth.ScopeWrite,

	// --- Channels (read/write/admin) ---
	"channels.list":    auth.ScopeRead,
	"channels.get":     auth.ScopeRead,
	"channels.status":  auth.ScopeRead,
	"channels.health":  auth.ScopeRead,
	"channels.start":   auth.ScopeAdmin,
	"channels.stop":    auth.ScopeAdmin,
	"channels.restart": auth.ScopeAdmin,
	"channels.logout":  auth.ScopeWrite,

	// --- Messaging ---
	"send":        auth.ScopeWrite,
	"poll":        auth.ScopeWrite,
	"talk.config": auth.ScopeRead,
	"talk.mode":   auth.ScopeWrite,

	// --- Agent (read/write) ---
	"agent":              auth.ScopeWrite,
	"agent.identity.get": auth.ScopeRead,
	"agent.wait":         auth.ScopeRead,
	"agent.status":       auth.ScopeRead,

	// --- Agents CRUD ---
	"agents.list":       auth.ScopeRead,
	"agents.create":     auth.ScopeWrite,
	"agents.update":     auth.ScopeWrite,
	"agents.delete":     auth.ScopeWrite,
	"agents.files.list": auth.ScopeRead,
	"agents.files.get":  auth.ScopeRead,
	"agents.files.set":  auth.ScopeWrite,

	// --- Skills ---
	"skills.status":           auth.ScopeRead,
	"skills.bins":             auth.ScopeRead,
	"skills.install":          auth.ScopeWrite,
	"skills.update":           auth.ScopeWrite,
	"skills.snapshot":         auth.ScopeRead,
	"skills.commands":         auth.ScopeRead,
	"skills.discover":         auth.ScopeRead,
	"skills.workspace_status": auth.ScopeRead,
	"skills.entries":          auth.ScopeRead,
	"tools.catalog":           auth.ScopeRead,

	// --- Process (write) ---
	"process.exec": auth.ScopeApprovals,
	"process.kill": auth.ScopeWrite,
	"process.get":  auth.ScopeRead,
	"process.list": auth.ScopeRead,

	// --- Cron (read/write) ---
	"wake":            auth.ScopeWrite,
	"cron.list":       auth.ScopeRead,
	"cron.get":        auth.ScopeRead,
	"cron.status":     auth.ScopeRead,
	"cron.runs":       auth.ScopeRead,
	"cron.add":        auth.ScopeWrite,
	"cron.update":     auth.ScopeWrite,
	"cron.remove":     auth.ScopeWrite,
	"cron.run":        auth.ScopeWrite,
	"cron.unregister": auth.ScopeWrite,

	// --- Autonomous (read/write) ---
	"autonomous.status":       auth.ScopeRead,
	"autonomous.goals.list":   auth.ScopeRead,
	"autonomous.goals.add":    auth.ScopeWrite,
	"autonomous.goals.remove": auth.ScopeWrite,
	"autonomous.cycle.run":    auth.ScopeWrite,
	"autonomous.cycle.stop":   auth.ScopeWrite,

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
	"chat.btw":     auth.ScopeWrite,

	// --- Monitoring (read) ---
	"monitoring.channel_health": auth.ScopeRead,
	"monitoring.activity":       auth.ScopeRead,

	// --- Event subscriptions (read) ---
	"node.event":                   auth.ScopeWrite,
	"subscribe.session":            auth.ScopeRead,
	"unsubscribe.session":          auth.ScopeRead,
	"subscribe.session.messages":   auth.ScopeRead,
	"unsubscribe.session.messages": auth.ScopeRead,

	// --- Wizard ---
	"wizard.start":  auth.ScopeWrite,
	"wizard.next":   auth.ScopeWrite,
	"wizard.cancel": auth.ScopeWrite,
	"wizard.status": auth.ScopeRead,

	// --- Media ---
	"browser.request": auth.ScopeWrite,

	// --- Web Login ---
	"web.login.start": auth.ScopeWrite,
	"web.login.wait":  auth.ScopeRead,

	// --- Exec Approvals ---
	"exec.approvals.get":         auth.ScopeRead,
	"exec.approvals.set":         auth.ScopeAdmin,
	"exec.approvals.node.get":    auth.ScopeRead,
	"exec.approvals.node.set":    auth.ScopeAdmin,
	"exec.approval.request":      auth.ScopeApprovals,
	"exec.approval.waitDecision": auth.ScopeApprovals,
	"exec.approval.resolve":      auth.ScopeApprovals,

	// --- Nodes ---
	"node.pair.request":              auth.ScopeWrite,
	"node.pair.list":                 auth.ScopeRead,
	"node.pair.approve":              auth.ScopeAdmin,
	"node.pair.reject":               auth.ScopeAdmin,
	"node.pair.verify":               auth.ScopeRead,
	"node.list":                      auth.ScopeRead,
	"node.describe":                  auth.ScopeRead,
	"node.rename":                    auth.ScopeWrite,
	"node.invoke":                    auth.ScopeWrite,
	"node.invoke.result":             auth.ScopeWrite,
	"node.canvas.capability.refresh": auth.ScopeWrite,
	"node.pending.pull":              auth.ScopeRead,
	"node.pending.ack":               auth.ScopeWrite,
	"node.pending.drain":             auth.ScopeAdmin,
	"node.pending.enqueue":           auth.ScopeWrite,

	// --- Device ---
	"device.pair.list":    auth.ScopeRead,
	"device.pair.approve": auth.ScopeAdmin,
	"device.pair.reject":  auth.ScopeAdmin,
	"device.pair.remove":  auth.ScopeAdmin,
	"device.token.rotate": auth.ScopeAdmin,
	"device.token.revoke": auth.ScopeAdmin,

	// --- Secrets ---
	"secrets.reload":  auth.ScopeAdmin,
	"secrets.resolve": auth.ScopeAdmin,

	// --- Usage ---
	"usage.status": auth.ScopeRead,
	"usage.cost":   auth.ScopeRead,

	// --- Security & Media (read) ---
	"protocol.validate":             auth.ScopeRead,
	"security.validate_session_key": auth.ScopeRead,
	"security.sanitize_html":        auth.ScopeRead,
	"security.is_safe_url":          auth.ScopeRead,
	"security.validate_error_code":  auth.ScopeRead,
	"media.detect_mime":             auth.ScopeRead,
	"parsing.extract_links":         auth.ScopeRead,
	"parsing.html_to_markdown":      auth.ScopeRead,
	"parsing.base64_estimate":       auth.ScopeRead,
	"parsing.base64_canonicalize":   auth.ScopeRead,
	"parsing.media_tokens":          auth.ScopeRead,

	// --- Memory search (read) ---
	"memory.cosine_similarity":    auth.ScopeRead,
	"memory.bm25_rank_to_score":   auth.ScopeRead,
	"memory.build_fts_query":      auth.ScopeRead,
	"memory.merge_hybrid_results": auth.ScopeRead,
	"memory.extract_keywords":     auth.ScopeRead,

	// --- Markdown (read) ---
	"markdown.to_ir":         auth.ScopeRead,
	"markdown.detect_fences": auth.ScopeRead,

	// --- Protocol validation (read) ---
	"protocol.validate_params": auth.ScopeRead,

	// --- Compaction (write) ---
	"compaction.evaluate":    auth.ScopeWrite,
	"compaction.sweep.new":   auth.ScopeWrite,
	"compaction.sweep.start": auth.ScopeWrite,
	"compaction.sweep.step":  auth.ScopeWrite,
	"compaction.sweep.drop":  auth.ScopeWrite,

	// --- Context engine (write) ---
	"context.assembly.new":   auth.ScopeWrite,
	"context.assembly.start": auth.ScopeWrite,
	"context.assembly.step":  auth.ScopeWrite,
	"context.expand.new":     auth.ScopeWrite,
	"context.expand.start":   auth.ScopeWrite,
	"context.expand.step":    auth.ScopeWrite,
	"context.engine.drop":    auth.ScopeWrite,

	// --- ML (read) ---
	"ml.embed":  auth.ScopeRead,
	"ml.rerank": auth.ScopeRead,

	// --- Providers (read) ---
	"providers.list":    auth.ScopeRead,
	"providers.catalog": auth.ScopeRead,

	// --- Vega (write) ---
	"vega.ask":         auth.ScopeWrite,
	"vega.update":      auth.ScopeWrite,
	"vega.add-action":  auth.ScopeWrite,
	"vega.mail-append": auth.ScopeWrite,
	"vega.version":     auth.ScopeRead,
	"vega.ffi.execute": auth.ScopeWrite,
	"vega.ffi.search":  auth.ScopeRead,

	// --- Config (admin) ---
	"config.get":           auth.ScopeAdmin,
	"config.set":           auth.ScopeAdmin,
	"config.patch":         auth.ScopeAdmin,
	"config.apply":         auth.ScopeAdmin,
	"config.reload":        auth.ScopeAdmin,
	"config.schema":        auth.ScopeRead,
	"config.schema.lookup": auth.ScopeRead,

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
