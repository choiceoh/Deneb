package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/device"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/node"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/secret"
	"github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/talk"
	"github.com/choiceoh/deneb/gateway-go/internal/wizard"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// tsBaseMethods is the canonical method list from src/gateway/server-methods-list.ts.
// Every method here must be registered in the Go dispatcher.
var tsBaseMethods = []string{
	"health",
	"doctor.memory.status",
	"logs.tail",
	"channels.status",
	"channels.logout",
	"status",
	"usage.status",
	"usage.cost",
	"tts.status",
	"tts.providers",
	"tts.enable",
	"tts.disable",
	"tts.convert",
	"tts.setProvider",
	"config.get",
	"config.set",
	"config.apply",
	"config.patch",
	"config.schema",
	"config.schema.lookup",
	"exec.approvals.get",
	"exec.approvals.set",
	"exec.approvals.node.get",
	"exec.approvals.node.set",
	"exec.approval.request",
	"exec.approval.waitDecision",
	"exec.approval.resolve",
	"wizard.start",
	"wizard.next",
	"wizard.cancel",
	"wizard.status",
	"talk.config",
	"talk.mode",
	"models.list",
	"tools.catalog",
	"agents.list",
	"agents.create",
	"agents.update",
	"agents.delete",
	"agents.files.list",
	"agents.files.get",
	"agents.files.set",
	"skills.status",
	"skills.bins",
	"skills.install",
	"skills.update",
	"update.run",
	"voicewake.get",
	"voicewake.set",
	"secrets.reload",
	"secrets.resolve",
	"sessions.list",
	"sessions.subscribe",
	"sessions.unsubscribe",
	"sessions.messages.subscribe",
	"sessions.messages.unsubscribe",
	"sessions.preview",
	"sessions.create",
	"sessions.send",
	"sessions.abort",
	"sessions.patch",
	"sessions.reset",
	"sessions.delete",
	"sessions.compact",
	"last-heartbeat",
	"set-heartbeats",
	"wake",
	"node.pair.request",
	"node.pair.list",
	"node.pair.approve",
	"node.pair.reject",
	"node.pair.verify",
	"device.pair.list",
	"device.pair.approve",
	"device.pair.reject",
	"device.pair.remove",
	"device.token.rotate",
	"device.token.revoke",
	"node.rename",
	"node.list",
	"node.describe",
	"node.pending.drain",
	"node.pending.enqueue",
	"node.invoke",
	"node.pending.pull",
	"node.pending.ack",
	"node.invoke.result",
	"node.event",
	"node.canvas.capability.refresh",
	"cron.list",
	"cron.status",
	"cron.add",
	"cron.update",
	"cron.remove",
	"cron.run",
	"cron.runs",
	"gateway.identity.get",
	"system-presence",
	"system-event",
	"send",
	"agent",
	"agent.identity.get",
	"agent.wait",
	"browser.request",
	"chat.history",
	"chat.abort",
	"chat.send",
}

// fullDispatcher creates a dispatcher with all registration paths wired up.
func fullDispatcher() *Dispatcher {
	d := NewDispatcher(testLogger())

	deps := testDeps()
	RegisterBuiltinMethods(d, deps)
	RegisterExtendedMethods(d, ExtendedDeps{
		Deps:      deps,
		Processes: process.NewManager(testLogger()),
		Cron:      cron.NewScheduler(testLogger()),
		Hooks:     hooks.NewRegistry(testLogger()),
	})

	// Phase 3: Native workflow methods (previously bridge-forwarded).
	broadcastFn := func(event string, payload any) (int, []error) { return 0, nil }
	RegisterApprovalMethods(d, ApprovalDeps{Store: approval.NewStore(), Broadcaster: broadcastFn})
	RegisterNodeMethods(d, NodeDeps{Nodes: node.NewManager(), Broadcaster: broadcastFn})
	RegisterDeviceMethods(d, DeviceDeps{Devices: device.NewManager(), Broadcaster: broadcastFn})
	RegisterCronAdvancedMethods(d, CronAdvancedDeps{Cron: cron.NewScheduler(testLogger()), Broadcaster: broadcastFn})
	RegisterAgentsMethods(d, AgentsDeps{Agents: agent.NewStore(), Broadcaster: broadcastFn})
	RegisterConfigAdvancedMethods(d, ConfigAdvancedDeps{Broadcaster: broadcastFn})
	RegisterSkillMethods(d, SkillDeps{Skills: skill.NewManager(), Broadcaster: broadcastFn})
	RegisterWizardMethods(d, WizardDeps{Engine: wizard.NewEngine()})
	RegisterSecretMethods(d, SecretDeps{Resolver: secret.NewResolver()})
	RegisterTalkMethods(d, TalkDeps{Talk: talk.NewState()})

	// Bridge methods with a nil-returning forwarder (for registration only).
	RegisterBridgeMethods(d, BridgeDeps{
		ForwarderFunc: func() Forwarder { return nil },
	})

	// Stub for methods registered outside the rpc package (events, chat, server inline).
	stub := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"ok": true})
		return resp
	}

	// Event subscription methods (normally via RegisterEventsMethods with Broadcaster).
	for _, m := range []string{
		"subscribe.session", "unsubscribe.session",
		"subscribe.session.messages", "unsubscribe.session.messages",
		"sessions.subscribe", "sessions.unsubscribe",
		"sessions.messages.subscribe", "sessions.messages.unsubscribe",
	} {
		d.Register(m, stub)
	}

	// Chat methods (normally via RegisterChatMethods with chat.Handler).
	for _, m := range []string{"chat.send", "chat.history", "chat.abort", "chat.inject"} {
		d.Register(m, stub)
	}

	// Server inline methods (normally in server.registerBuiltinMethods).
	for _, m := range []string{
		"health", "status", "config.get",
		"daemon.status", "events.broadcast",
		"gateway.identity.get", "last-heartbeat", "set-heartbeats",
		"system-presence", "system-event", "models.list",
	} {
		d.Register(m, stub)
	}

	return d
}

// TestTSBaseMethodParity verifies every method from the TS BASE_METHODS list
// is registered in the Go dispatcher.
func TestTSBaseMethodParity(t *testing.T) {
	d := fullDispatcher()
	registered := make(map[string]bool)
	for _, m := range d.Methods() {
		registered[m] = true
	}

	var missing []string
	for _, m := range tsBaseMethods {
		if !registered[m] {
			missing = append(missing, m)
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("TS BASE_METHODS not registered in Go dispatcher (%d missing):\n", len(missing))
		for _, m := range missing {
			t.Errorf("  - %s", m)
		}
	}
}

// TestScopeCoverage verifies every registered method has an entry in methodScopes.
func TestScopeCoverage(t *testing.T) {
	d := fullDispatcher()
	var uncovered []string
	for _, m := range d.Methods() {
		if _, ok := methodScopes[m]; !ok {
			uncovered = append(uncovered, m)
		}
	}

	if len(uncovered) > 0 {
		sort.Strings(uncovered)
		t.Errorf("registered methods without scope mapping (%d):\n", len(uncovered))
		for _, m := range uncovered {
			t.Errorf("  - %s", m)
		}
	}
}

// TestBridgeForwardSuccess verifies bridge-forwarded methods delegate to the forwarder.
func TestBridgeForwardSuccess(t *testing.T) {
	d := NewDispatcher(testLogger())
	fwd := &mockForwarder{}
	RegisterBridgeMethods(d, BridgeDeps{
		ForwarderFunc: func() Forwarder { return fwd },
	})

	// Pick a bridge-forwarded method (still forwarded after Phase 3 port).
	req := &protocol.RequestFrame{
		Type:   "req",
		ID:     "bridge-1",
		Method: "send",
		Params: json.RawMessage("{}"),
	}
	resp := d.Dispatch(context.Background(), req)

	if !fwd.called {
		t.Error("forwarder should have been called for bridge method")
	}
	if !resp.OK {
		t.Errorf("expected OK response, got error: %+v", resp.Error)
	}
}

// TestBridgeForwardNilBridge verifies graceful error when bridge is not connected.
func TestBridgeForwardNilBridge(t *testing.T) {
	d := NewDispatcher(testLogger())
	RegisterBridgeMethods(d, BridgeDeps{
		ForwarderFunc: func() Forwarder { return nil },
	})

	req := &protocol.RequestFrame{
		Type:   "req",
		ID:     "bridge-nil-1",
		Method: "send",
		Params: json.RawMessage("{}"),
	}
	resp := d.Dispatch(context.Background(), req)

	if resp.OK {
		t.Error("expected error when bridge is nil")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrDependencyFailed {
		t.Errorf("expected DEPENDENCY_FAILED error, got: %+v", resp.Error)
	}
}

// mockFailingForwarder returns an error on Forward.
type mockFailingForwarder struct{}

func (m *mockFailingForwarder) Forward(_ context.Context, _ *protocol.RequestFrame) (*protocol.ResponseFrame, error) {
	return nil, errors.New("connection lost")
}

// TestBridgeForwardError verifies error handling when bridge returns an error.
func TestBridgeForwardError(t *testing.T) {
	d := NewDispatcher(testLogger())
	RegisterBridgeMethods(d, BridgeDeps{
		ForwarderFunc: func() Forwarder { return &mockFailingForwarder{} },
	})

	req := &protocol.RequestFrame{
		Type:   "req",
		ID:     "bridge-err-1",
		Method: "tts.status",
		Params: json.RawMessage("{}"),
	}
	resp := d.Dispatch(context.Background(), req)

	if resp.OK {
		t.Error("expected error response when forward fails")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrDependencyFailed {
		t.Errorf("expected DEPENDENCY_FAILED error, got: %+v", resp.Error)
	}
}

// TestMethodCount verifies the total number of registered methods meets the target.
func TestMethodCount(t *testing.T) {
	d := fullDispatcher()
	methods := d.Methods()
	// We expect at least 99 methods (TS BASE_METHODS has 113, plus Go-only methods).
	if len(methods) < 99 {
		t.Errorf("expected at least 99 registered methods, got %d", len(methods))
	}
	t.Logf("total registered methods: %d", len(methods))
}
