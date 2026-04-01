package server

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
)

// requiredMethods lists RPC methods that MUST be registered after full server
// initialization. If a method disappears (e.g. removed from method_registry.go
// without updating handlers), this test catches it immediately.
//
// Grouped by domain — keep alphabetical within each group.
var requiredMethods = []string{
	// Agent.
	"agent.status",
	"agents.create",
	"agents.delete",
	"agents.files.get",
	"agents.files.list",
	"agents.files.set",
	"agents.list",
	"agents.update",

	// ACP.
	"acp.bind",
	"acp.bindings",
	"acp.kill",
	"acp.list",
	"acp.send",
	"acp.spawn",
	"acp.start",
	"acp.status",
	"acp.stop",
	"acp.unbind",

	// Chat.
	"chat.abort",
	"chat.btw",
	"chat.history",
	"chat.inject",
	"chat.send",

	// Session.
	"sessions.abort",
	"sessions.compact",
	"sessions.create",
	"sessions.lifecycle",
	"sessions.patch",
	"sessions.preview",
	"sessions.repair",
	"sessions.reset",
	"sessions.resolve",
	"sessions.send",
	"sessions.steer",

	// Channel events (channels.start/stop/restart are conditional on Telegram config).
	"events.broadcast",
	"subscribe.session",
	"subscribe.session.messages",
	"unsubscribe.session",
	"unsubscribe.session.messages",

	// Background task control plane.
	"flow.cancel",
	"flow.list",
	"flow.show",
	"task.audit",
	"task.cancel",
	"task.events",
	"task.get",
	"task.list",
	"task.status",

	// Process and cron.
	"cron.add",
	"cron.get",
	"cron.list",
	"cron.remove",
	"cron.run",
	"cron.runs",
	"cron.status",
	"cron.unregister",
	"cron.update",
	"exec.approval.request",
	"exec.approval.resolve",
	"exec.approval.waitDecision",
	"exec.approvals.get",
	"exec.approvals.set",
	"process.exec",
	"process.get",
	"process.kill",
	"process.list",

	// Hooks.
	"hooks.fire",
	"hooks.list",
	"hooks.register",
	"hooks.unregister",

	// Skills.
	"skills.bins",
	"skills.commands",
	"skills.discover",
	"skills.entries",
	"skills.install",
	"skills.snapshot",
	"skills.status",
	"skills.update",
	"skills.workspace_status",
	"tools.catalog",
	"tools.invoke",
	"tools.list",
	"tools.status",

	// System.
	"config.apply",
	"config.get",
	"config.patch",
	"config.reload",
	"config.schema",
	"config.set",
	"gateway.identity.get",
	"logs.tail",
	"maintenance.run",
	"maintenance.status",
	"maintenance.summary",
	"monitoring.activity",
	"monitoring.channel_health",
	"monitoring.rpc_zero_calls",
	"update.run",
	"usage.cost",
	"usage.status",

	// Presence.
	"last-heartbeat",
	"set-heartbeats",
	"system-event",
	"system-presence",

	// Platform.
	"secrets.reload",
	"secrets.resolve",
	"talk.config",
	"talk.mode",
	"wizard.cancel",
	"wizard.next",
	"wizard.start",
	"wizard.status",

	// Aurora.
	"aurora.chat",
	"aurora.memory",
	"aurora.ping",

	// Gateway builtins.
	"status",
}

// TestMethodRegistry_RequiredMethodsRegistered verifies that all required RPC
// methods are registered after server.New(). If this test fails, a method was
// likely removed from method_registry.go without removing it from the handler.
func TestMethodRegistry_RequiredMethodsRegistered(t *testing.T) {
	srv := New(":0")
	registered := make(map[string]bool)
	for _, m := range srv.dispatcher.Methods() {
		registered[m] = true
	}

	var missing []string
	for _, m := range requiredMethods {
		if !registered[m] {
			missing = append(missing, m)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("required RPC methods not registered (%d missing):\n", len(missing))
		for _, m := range missing {
			t.Errorf("  - %s", m)
		}
	}
}

// TestWiringRules_HandlersDoNotImportHub enforces Rule 3: handler packages
// must not import rpcutil.GatewayHub. Scans Go source files for violations.
func TestWiringRules_HandlersDoNotImportHub(t *testing.T) {
	handlerDir := filepath.Join("..", "rpc", "handler")
	err := filepath.Walk(handlerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		content := string(data)
		if strings.Contains(content, "rpcutil.GatewayHub") || strings.Contains(content, "*rpcutil.GatewayHub") {
			rel, _ := filepath.Rel(handlerDir, path)
			t.Errorf("handler %s imports/references GatewayHub (Rule 3 violation)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk handler dir: %v", err)
	}
}

// TestWiringRules_ValidateHub verifies that Validate() catches missing required fields.
func TestWiringRules_ValidateHub(t *testing.T) {
	// Empty hub (via zero-value config) should fail validation.
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})
	if err := hub.Validate(); err == nil {
		t.Fatal("expected validation error for empty hub, got nil")
	}
}

// TestWiringRules_PhaseOrdering verifies that AdvancePhase panics on out-of-order calls.
func TestWiringRules_PhaseOrdering(t *testing.T) {
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})

	// Skipping PhaseEarly and jumping to PhaseSession should panic.
	assertPanics(t, "skip PhaseEarly", func() {
		hub.AdvancePhase(rpcutil.PhaseSession)
	})

	// Normal progression should not panic.
	hub.AdvancePhase(rpcutil.PhaseEarly)
	if hub.Phase() != rpcutil.PhaseEarly {
		t.Fatalf("expected PhaseEarly, got %d", hub.Phase())
	}

	hub.AdvancePhase(rpcutil.PhaseSession)
	if hub.Phase() != rpcutil.PhaseSession {
		t.Fatalf("expected PhaseSession, got %d", hub.Phase())
	}

	hub.AdvancePhase(rpcutil.PhaseLate)
	if hub.Phase() != rpcutil.PhaseLate {
		t.Fatalf("expected PhaseLate, got %d", hub.Phase())
	}

	// Going backwards should panic.
	assertPanics(t, "backwards to PhaseEarly", func() {
		hub.AdvancePhase(rpcutil.PhaseEarly)
	})
}

// TestWiringRules_SetChatBeforeSessionPhase verifies that SetChat panics before PhaseSession.
func TestWiringRules_SetChatBeforeSessionPhase(t *testing.T) {
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})

	// SetChat before PhaseSession should panic.
	assertPanics(t, "SetChat at PhaseInit", func() {
		hub.SetChat(nil)
	})

	hub.AdvancePhase(rpcutil.PhaseEarly)
	assertPanics(t, "SetChat at PhaseEarly", func() {
		hub.SetChat(nil)
	})

	// After PhaseSession, SetChat should succeed.
	hub.AdvancePhase(rpcutil.PhaseSession)
	hub.SetChat(nil) // should not panic
}

func assertPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: expected panic, got none", name)
		}
	}()
	fn()
}
