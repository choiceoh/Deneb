package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// requiredMethods lists RPC methods that MUST be registered after full server
// initialization. If a method disappears (e.g. removed from method_registry.go
// without updating handlers), this test catches it immediately.
//
// Grouped by domain — keep alphabetical within each group.
var requiredMethods = []string{
	// Agent.
	"agent.status",

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
	"chat.send",
	"chat.steer",

	// Session.
	"sessions.abort",
	"sessions.create",
	"sessions.lifecycle",
	"sessions.patch",
	"sessions.reset",
	"sessions.send",
	"sessions.steer",

	// Event broadcast + session subscription.
	"events.broadcast",
	"subscribe.session",
	"subscribe.session.messages",
	"unsubscribe.session",
	"unsubscribe.session.messages",

	// Background task control plane.

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
	"process.exec",
	"process.get",
	"process.kill",
	"process.list",

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

	// Insights.
	"insights.generate",

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
	"monitoring.channel_health",
	"monitoring.rpc_zero_calls",
	"observatory.snapshot",
	"update.run",
	"usage.cost",
	"usage.status",

	// Wiki: feature-flagged (DENEB_WIKI_ENABLED), not in required list.

	// Mini App (Telegram).
	"miniapp.ping",
	// miniapp.mail.* — the archive-first mail namespace (the legacy
	// miniapp.gmail.* aliases were removed once both native clients migrated).
	"miniapp.mail.list_recent",
	"miniapp.mail.get",
	"miniapp.mail.mark_read",
	"miniapp.mail.archive",
	"miniapp.mail.trash",
	"miniapp.mail.native_status",
	"miniapp.mail.sender_context",
	"miniapp.models.add_custom",
	"miniapp.models.delete_custom",
	"miniapp.models.list",
	"miniapp.models.set",
	"miniapp.usage.stats",
	"miniapp.files.list",
	"miniapp.files.search",
	"miniapp.files.share",
	"miniapp.files.upload",
	"miniapp.files.delete",
	"miniapp.files.mkdir",
	"miniapp.files.move",
	// miniapp.mail.analyze and miniapp.mail.analysis_cached are
	// conditional on an LLM client being configured
	// (modelRegistry.Client(RoleMain) returning non-nil) — not in the
	// required list because tests run without providers.
	"miniapp.sessions.recent",
	"miniapp.sessions.delete",
	"miniapp.sessions.rename",
	"miniapp.sessions.transcript",
	// FCM device-token registration — the token store always resolves (temp
	// state dir in tests), so these register unconditionally even though the
	// FCM sender stays dormant without credentials.
	"miniapp.push.register",
	"miniapp.push.unregister",
	// Skills catalog/detail/write surface + Propus feed — List is always wired,
	// so these register unconditionally (tracker absence degrades the payload,
	// not the registration).
	"miniapp.skills.list",
	"miniapp.skills.detail",
	"miniapp.skills.update",
	"miniapp.skills.delete",
	"miniapp.skills.lifecycle",
	// Part-status dashboard — the classifier Rules loader is always non-nil
	// (org.LoadRules falls back to the legacy classification path), so this
	// registers unconditionally even when no data source is wired in tests.
	"miniapp.dashboard.lanes",
	// RSI loop-status window (native + andromeda). The Status closure is always
	// non-nil (it degrades to an empty snapshot when the tracker is unwired), so
	// the read handler registers unconditionally.
	"miniapp.rsi.status",
	// Project digests — the store is a stateless dir wrapper (always non-nil),
	// so the read handler registers unconditionally (empty until the dream
	// cycle writes the first digest).
	"miniapp.project.digests",
	// Server-side project↔item matching (linked mail/work-feed/notebook IDs);
	// registers with project.digests under the same wiki factory.
	"miniapp.project.linked",
	// Org chart editor — Load/SavePath are always wired (org.Load / ResolvePath),
	// so these register unconditionally.
	"miniapp.org.get",
	"miniapp.org.save",
	// To-do domain — local store always resolves in tests (temp state dir),
	// so these always register.
	"miniapp.todo.list",
	"miniapp.todo.create",
	"miniapp.todo.update",
	"miniapp.todo.set_done",
	"miniapp.todo.delete",
	// Full address-book list (miniapp.contacts.list) — the contacts store
	// resolves in tests (temp state dir), so this always registers.
	"miniapp.contacts.list",
	// 시장 시세 (miniapp.market.summary) — keyless cache fetcher is always wired.
	"miniapp.market.summary",
	// 전자결재 browse/act/get/analyze + ERP list — always registered;
	// FromEnv fails the call when unset. Analyze needs an LLM at call time.
	"miniapp.groupware.approvals.list",
	"miniapp.groupware.approvals.act",
	"miniapp.groupware.approvals.get",
	"miniapp.groupware.approvals.attachment",
	"miniapp.groupware.approvals.analysis_cached",
	"miniapp.groupware.approvals.analyze",
	"miniapp.groupware.erp.list",
	// Single-topic background editor — registers whenever topics resolve
	// (the test harness loads the real deneb.json topics map {"0":"업무"}).
	"miniapp.topicdocs.read_current",
	"miniapp.topicdocs.write_current",
	// miniapp.memory.{search,get_page,categories,list_in_category,diary_recent}
	// are conditional on wiki being enabled (DENEB_WIKI_ENABLED) — omitted
	// here, matching the wiki.* exclusion above.
	// miniapp.notebook.{list,get} depend on s.notebookStore (set in late chat
	// init); omitted here for the same lazy-store reason as memory.
	// miniapp.crons.list registers only when the cron service is wired
	// (always true in production but the test harness can skip it).

	// Gateway builtins.
	"status",
}

// TestMethodRegistry_RequiredMethodsRegistered verifies that all required RPC
// methods are registered after server.New(). If this test fails, a method was
// likely removed from method_registry.go without removing it from the handler.
func TestMethodRegistryReturnsNoMissingRequiredMethods(t *testing.T) {
	t.Parallel()
	srv := sharedReadOnlyServer(t)
	registered := make(map[string]struct{})
	for _, m := range srv.dispatcher.Methods() {
		registered[m] = struct{}{}
	}

	var missing []string
	for _, m := range requiredMethods {
		if _, ok := registered[m]; !ok {
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

// TestMethodRegistry_ModelPickerSeesSessionState guards the phase contract for
// miniapp.models.*: the picker Controller snapshots s.modelRegistry and
// s.chatHandler at construction, so it must register in registerLateMethods,
// after registerSessionRPCMethods creates both. Registered early (#3457) the
// snapshots stayed nil — models.list reported roles=null and the native picker
// showed every role as 미설정 while models.set was rejected as "not ready".
// The role rows come from the registry (not provider config), so a populated
// roles list proves the controller saw the session-phase registry.
func TestMethodRegistryModelsListReturnsPopulatedRoles(t *testing.T) {
	t.Parallel()
	srv := sharedReadOnlyServer(t)
	ctx := clientauth.WithContext(context.Background(), &clientauth.Identity{})
	resp := srv.dispatcher.Dispatch(ctx, &protocol.RequestFrame{
		ID:     "test-models-roles",
		Method: "miniapp.models.list",
		Params: json.RawMessage(`{}`),
	})
	if resp == nil || !resp.OK {
		t.Fatalf("miniapp.models.list failed: %+v", resp)
	}
	var payload struct {
		Roles []struct {
			Role string `json:"role"`
		} `json:"roles"`
	}
	testutil.NoError(t, json.Unmarshal(resp.Payload, &payload))
	if len(payload.Roles) == 0 {
		t.Fatal("models.list returned no roles: picker controller was constructed before the model registry (early/late phase regression)")
	}
}

// TestWiringRules_HandlersDoNotImportHub enforces Rule 3: handler packages
// must not import rpcutil.GatewayHub. Scans Go source files for violations.
func TestWiringRulesFailsWhenHandlerImportsHub(t *testing.T) {
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
	testutil.NoError(t, err)
}

// TestWiringRules_ValidateHub verifies that Validate() catches missing required fields.
func TestWiringRulesValidateHubReturnsErrorForEmptyHub(t *testing.T) {
	// Empty hub (via zero-value config) should fail validation.
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})
	if err := hub.Validate(); err == nil {
		t.Fatal("got nil, want validation error for empty hub")
	}
}

// TestWiringRules_PhaseOrdering verifies that AdvancePhase panics on out-of-order calls.
func TestWiringRulesPhaseOrderingPanicsWhenOutOfOrder(t *testing.T) {
	hub := rpcutil.NewGatewayHub(rpcutil.HubConfig{})

	// Skipping PhaseEarly and jumping to PhaseSession should panic.
	assertPanics(t, "skip PhaseEarly", func() {
		hub.AdvancePhase(rpcutil.PhaseSession)
	})

	// Normal progression should not panic.
	hub.AdvancePhase(rpcutil.PhaseEarly)
	if hub.Phase() != rpcutil.PhaseEarly {
		t.Fatalf("got %d, want PhaseEarly", hub.Phase())
	}

	hub.AdvancePhase(rpcutil.PhaseSession)
	if hub.Phase() != rpcutil.PhaseSession {
		t.Fatalf("got %d, want PhaseSession", hub.Phase())
	}

	hub.AdvancePhase(rpcutil.PhaseLate)
	if hub.Phase() != rpcutil.PhaseLate {
		t.Fatalf("got %d, want PhaseLate", hub.Phase())
	}

	// Going backwards should panic.
	assertPanics(t, "backwards to PhaseEarly", func() {
		hub.AdvancePhase(rpcutil.PhaseEarly)
	})
}

func assertPanics(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s: got none, want panic", name)
		}
	}()
	fn()
}
