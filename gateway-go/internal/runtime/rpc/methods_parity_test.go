package rpc

import (
	"context"
	"sort"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/usage"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/secret"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// tsBaseMethods is the canonical method list from src/gateway/server-methods-list.ts.
// Every method here must be registered in the Go dispatcher.
var tsBaseMethods = []string{
	"health",
	"doctor.memory.status",
	"logs.tail",
	"telegram.status",
	"telegram.logout",
	"status",
	"usage.status",
	"usage.cost",
	"config.get",
	"config.set",
	"config.apply",
	"config.patch",
	"config.schema",
	"config.schema.lookup",
	"exec.approvals.get",
	"exec.approvals.set",
	"exec.approval.request",
	"exec.approval.waitDecision",
	"exec.approval.resolve",
	"models.list",
	"tools.catalog",
	"skills.status",
	"skills.bins",
	"skills.install",
	"skills.update",
	"update.run",
	"secrets.reload",
	"secrets.resolve",
	"sessions.list",
	"sessions.subscribe",
	"sessions.unsubscribe",
	"sessions.messages.subscribe",
	"sessions.messages.unsubscribe",
	"sessions.tools.subscribe",
	"sessions.tools.unsubscribe",
	"sessions.preview",
	"sessions.create",
	"sessions.send",
	"sessions.steer",
	"sessions.abort",
	"sessions.patch",
	"sessions.reset",
	"sessions.delete",
	"last-heartbeat",
	"set-heartbeats",
	"cron.list",
	"cron.listPage",
	"cron.getJob",
	"cron.status",
	"cron.add",
	"cron.update",
	"cron.remove",
	"cron.run",
	"cron.runs",
	"gateway.identity.get",
	"system-presence",
	"system-event",
	"agent",
	"agent.wait",
	"chat.history",
	"chat.abort",
	"chat.send",
}

// fullDispatcher creates a dispatcher with all registration paths wired up.
func fullDispatcher() *Dispatcher {
	d := NewDispatcher(testLogger())

	sm := session.NewManager()
	RegisterBuiltinMethods(d)
	RegisterSessionCRUDMethods(d, SessionDeps{Sessions: sm})
	RegisterTelegramStatusMethods(d, TelegramStatusDeps{})
	RegisterHealthMethods(d, SystemHealthDeps{})
	testCronService := cron.NewService(cron.ServiceConfig{StorePath: "/tmp/deneb-cron-test-ext"}, nil, testLogger())
	RegisterExtendedMethods(d, ExtendedDeps{
		Sessions:    sm,
		Processes:   process.NewManager(testLogger()),
		CronService: testCronService,
	})

	// Phase 3: Native workflow methods.
	broadcastFn := func(event string, payload any) (int, []error) { return 0, nil }
	RegisterApprovalMethods(d, ApprovalDeps{Store: approval.NewStore(), Broadcaster: broadcastFn})
	RegisterCronAdvancedMethods(d, CronAdvancedDeps{Service: cron.NewService(cron.ServiceConfig{StorePath: "/tmp/deneb-cron-test-adv"}, nil, testLogger()), Broadcaster: broadcastFn})
	RegisterCronServiceMethods(d, CronServiceDeps{Service: cron.NewService(cron.ServiceConfig{StorePath: "/tmp/deneb-cron-test"}, nil, testLogger())})
	RegisterConfigAdvancedMethods(d, ConfigAdvancedDeps{Broadcaster: broadcastFn})
	RegisterSkillMethods(d, SkillDeps{Skills: skill.NewManager(), Broadcaster: broadcastFn})
	RegisterSecretMethods(d, SecretDeps{Resolver: secret.NewResolver()})
	// Session state methods (patch/reset/preview/resolve/compact).
	RegisterSessionMethods(d, SessionDeps{
		Sessions: sm,
	})

	// Phase 4: Native system methods.
	RegisterUsageMethods(d, UsageDeps{Tracker: usage.New()})
	RegisterLogsMethods(d, LogsDeps{LogDir: "/tmp"})
	RegisterDoctorMethods(d, DoctorDeps{})
	RegisterMaintenanceMethods(d, MaintenanceDeps{Runner: maintenance.NewRunner("/tmp")})
	RegisterUpdateMethods(d, UpdateDeps{})
	// Phase 4: Native session execution / agent methods.
	RegisterSessionExecMethods(d, SessionExecDeps{
		Chat:       chat.NewHandler(session.NewManager(), nil, testLogger(), chat.DefaultHandlerConfig()),
		JobTracker: agent.NewJobTracker(testLogger()),
	})

	d.Register("telegram.logout", func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{"ok": true})
		return resp
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
		"sessions.tools.subscribe", "sessions.tools.unsubscribe",
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
	registered := make(map[string]struct{})
	for _, m := range d.Methods() {
		registered[m] = struct{}{}
	}

	var missing []string
	for _, m := range tsBaseMethods {
		if _, ok := registered[m]; !ok {
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

// TestMethodCount verifies the total number of registered methods meets the target.
func TestMethodCount(t *testing.T) {
	d := fullDispatcher()
	methods := d.Methods()
	// We expect at least 91 methods (TS BASE_METHODS minus removed agents CRUD/identity, plus Go-only methods).
	if len(methods) < 91 {
		t.Errorf("got %d, want at least 91 registered methods", len(methods))
	}
	t.Logf("total registered methods: %d", len(methods))
}
