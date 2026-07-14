package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	rtevents "github.com/choiceoh/deneb/gateway-go/internal/runtime/events"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func decodeProcessPayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	rpctest.MustOK(t, resp)
	var got T
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, resp.Payload)
	}
	return got
}

func TestACPMethodsNilAndCompleteSurface(t *testing.T) {
	if got := ACPMethods(nil); got != nil {
		t.Fatalf("ACPMethods(nil) = %#v", got)
	}
	deps := &ACPDeps{Registry: acp.NewACPRegistry()}
	methods := ACPMethods(deps)
	want := []string{
		"acp.status", "acp.start", "acp.stop", "acp.list", "acp.bindings",
		"acp.spawn", "acp.kill", "acp.send", "acp.bind", "acp.unbind",
	}
	for _, name := range want {
		if methods[name] == nil {
			t.Errorf("missing %s", name)
		}
	}
	if len(methods) != len(want) {
		t.Fatalf("method count = %d, want %d", len(methods), len(want))
	}
}

func TestACPStatusReturnsAgentAndBindingCounts(t *testing.T) {
	registry := acp.NewACPRegistry()
	registry.Register(acp.ACPAgent{ID: "idle", ParentID: "p", Status: "idle"})
	registry.Register(acp.ACPAgent{ID: "running", ParentID: "p", Status: "running"})
	registry.Register(acp.ACPAgent{ID: "done", ParentID: "p", Status: "done"})
	bindings := acp.NewSessionBindingService()
	bindings.Bind(acp.SessionBindParams{TargetSessionKey: "client:main", Channel: "telegram", ConversationID: "1"})
	deps := &ACPDeps{Registry: registry, Bindings: bindings}
	methods := ACPMethods(deps)

	status := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.status", nil))
	if status["enabled"] != false || status["totalAgents"] != float64(3) ||
		status["activeAgents"] != float64(2) || status["bindings"] != float64(1) {
		t.Fatalf("disabled status = %#v", status)
	}

	startAt := time.Now().UnixMilli()
	started := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.start", nil))
	if started["enabled"] != true || started["wasAlready"] != false || started["startedAtEpoch"].(float64) < float64(startAt) || !deps.IsEnabled() {
		t.Fatalf("first start = %#v enabled=%v", started, deps.IsEnabled())
	}
	startedAgain := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.start", nil))
	if startedAgain["wasAlready"] != true {
		t.Fatalf("second start = %#v", startedAgain)
	}

	stopAt := time.Now().UnixMilli()
	stopped := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.stop", nil))
	if stopped["enabled"] != false || stopped["wasEnabled"] != true || stopped["stoppedAtEpoch"].(float64) < float64(stopAt) || deps.IsEnabled() {
		t.Fatalf("first stop = %#v enabled=%v", stopped, deps.IsEnabled())
	}
	stoppedAgain := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.stop", nil))
	if stoppedAgain["wasEnabled"] != false {
		t.Fatalf("second stop = %#v", stoppedAgain)
	}
}

func TestACPStartStopConcurrentAtomicResponses(t *testing.T) {
	deps := &ACPDeps{Registry: acp.NewACPRegistry()}
	methods := ACPMethods(deps)
	const workers = 40
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			method := "acp.start"
			if i%2 == 1 {
				method = "acp.stop"
			}
			resp := rpctest.Call(methods, method, nil)
			if resp == nil || resp.Error != nil {
				t.Errorf("%s response = %#v", method, resp)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	// The final value depends on scheduling, but must remain a valid atomic bool
	// and a subsequent operation must observe and replace it consistently.
	before := deps.IsEnabled()
	stop := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.stop", nil))
	if stop["wasEnabled"] != before || deps.IsEnabled() {
		t.Fatalf("post-concurrency stop = %#v before=%v", stop, before)
	}
}

func TestACPListParentFilterAndMalformedParamsFallback(t *testing.T) {
	registry := acp.NewACPRegistry()
	registry.Register(acp.ACPAgent{ID: "a", ParentID: "p1", Status: "idle"})
	registry.Register(acp.ACPAgent{ID: "b", ParentID: "p1", Status: "done"})
	registry.Register(acp.ACPAgent{ID: "c", ParentID: "p2", Status: "running"})
	methods := ACPMethods(&ACPDeps{Registry: registry})
	type payload struct {
		Agents []acp.ACPAgent `json:"agents"`
		Count  int            `json:"count"`
	}
	filtered := decodeProcessPayload[payload](t, rpctest.Call(methods, "acp.list", map[string]any{"parentId": "p1"}))
	if filtered.Count != 2 || len(filtered.Agents) != 2 {
		t.Fatalf("filtered list = %#v", filtered)
	}

	resp := methods["acp.list"](context.Background(), &protocol.RequestFrame{
		ID: "bad", Method: "acp.list", Params: json.RawMessage(`{"parentId":`),
	})
	all := decodeProcessPayload[payload](t, resp)
	if all.Count != 3 || len(all.Agents) != 3 {
		t.Fatalf("malformed list fallback = %#v", all)
	}
}

func TestACPSendResolutionValidationAndDependencyFailures(t *testing.T) {
	registry := acp.NewACPRegistry()
	registry.Register(acp.ACPAgent{ID: "worker", Status: "running", SessionKey: "acp:parent:worker"})
	deps := &ACPDeps{Registry: registry}
	deps.SetEnabled(true)
	methods := ACPMethods(deps)

	cases := []struct {
		name   string
		params map[string]any
		code   string
	}{
		{"missing message", map[string]any{"agentId": "worker"}, protocol.ErrMissingParam},
		{"missing target", map[string]any{"message": "hello"}, protocol.ErrMissingParam},
		{"unknown agent", map[string]any{"agentId": "missing", "message": "hello"}, protocol.ErrNotFound},
		{"missing sender", map[string]any{"agentId": "worker", "message": "hello"}, protocol.ErrDependencyFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := rpctest.Call(methods, "acp.send", tc.params)
			rpctest.MustErr(t, resp)
			if resp.Error.Code != tc.code {
				t.Fatalf("code = %q, want %q", resp.Error.Code, tc.code)
			}
		})
	}

	wantErr := errors.New("queue closed")
	deps.SessionSendFn = func(string, string) error { return wantErr }
	resp := rpctest.Call(methods, "acp.send", map[string]any{"sessionKey": "direct", "message": "hello"})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrDependencyFailed || !strings.Contains(resp.Error.Message, wantErr.Error()) {
		t.Fatalf("send failure = %#v", resp.Error)
	}

	var gotKey, gotMessage string
	deps.SessionSendFn = func(key, message string) error {
		gotKey, gotMessage = key, message
		return nil
	}
	sent := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.send", map[string]any{
		"agentId": "worker", "message": "review this",
	}))
	if sent["sent"] != true || sent["sessionKey"] != "acp:parent:worker" || gotKey != "acp:parent:worker" || gotMessage != "review this" {
		t.Fatalf("agent send = %#v captured=%q,%q", sent, gotKey, gotMessage)
	}

	sent = decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.send", map[string]any{
		"agentId": "missing", "sessionKey": "explicit", "message": "wins",
	}))
	if sent["sessionKey"] != "explicit" || gotKey != "explicit" {
		t.Fatalf("explicit session precedence = %#v captured=%q", sent, gotKey)
	}
}

func TestACPKillRegistryFallback(t *testing.T) {
	registry := acp.NewACPRegistry()
	registry.Register(acp.ACPAgent{ID: "worker", Status: "running"})
	deps := &ACPDeps{Registry: registry}
	deps.SetEnabled(true)
	methods := ACPMethods(deps)

	for _, id := range []string{"", "missing"} {
		resp := rpctest.Call(methods, "acp.kill", map[string]any{"agentId": id})
		rpctest.MustErr(t, resp)
		want := protocol.ErrMissingParam
		if id != "" {
			want = protocol.ErrNotFound
		}
		if resp.Error.Code != want {
			t.Errorf("kill %q code = %q, want %q", id, resp.Error.Code, want)
		}
	}
	killed := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.kill", map[string]any{"agentId": "worker"}))
	if killed["killed"] != true || killed["agentId"] != "worker" || registry.Get("worker").Status != "killed" {
		t.Fatalf("kill result = %#v agent=%#v", killed, registry.Get("worker"))
	}
}

func TestACPBindUnbindRoundTripAndPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bindings.json")
	bindings := acp.NewSessionBindingService()
	deps := &ACPDeps{
		Registry:     acp.NewACPRegistry(),
		Bindings:     bindings,
		BindingStore: acp.NewBindingStore(path),
	}
	deps.SetEnabled(true)
	methods := ACPMethods(deps)

	resp := rpctest.Call(methods, "acp.bind", map[string]any{})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("missing bind target code = %q", resp.Error.Code)
	}
	bound := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.bind", map[string]any{
		"channel": "telegram", "accountId": "default", "conversationId": "42",
		"targetSessionKey": "client:main", "boundBy": "owner",
	}))
	bindingID, _ := bound["bindingId"].(string)
	if bindingID == "" || bound["conversationId"] != "42" || bound["targetKey"] != "client:main" {
		t.Fatalf("bind result = %#v", bound)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("bind store not persisted: %v", err)
	}

	all := decodeProcessPayload[map[string]json.RawMessage](t, rpctest.Call(methods, "acp.bindings", nil))
	var allCount int
	if err := json.Unmarshal(all["count"], &allCount); err != nil || allCount != 1 {
		t.Fatalf("all binding count = %d, %v raw=%s", allCount, err, all["count"])
	}
	filtered := decodeProcessPayload[map[string]json.RawMessage](t, rpctest.Call(methods, "acp.bindings", map[string]any{"sessionKey": "client:main"}))
	var filteredCount int
	_ = json.Unmarshal(filtered["count"], &filteredCount)
	if filteredCount != 1 {
		t.Fatalf("filtered binding count = %d", filteredCount)
	}

	unbound := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "acp.unbind", map[string]any{
		"channel": "telegram", "accountId": "default", "conversationId": "42",
	}))
	if unbound["unbound"] != true || unbound["bindingId"] != bindingID {
		t.Fatalf("conversation unbind = %#v", unbound)
	}
	if got := bindings.Resolve("telegram", "default", "42"); got != nil {
		t.Fatalf("binding survived unbind: %#v", got)
	}
	resp = rpctest.Call(methods, "acp.unbind", map[string]any{"bindingId": bindingID})
	rpctest.MustErr(t, resp)
	if resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("stale unbind code = %q", resp.Error.Code)
	}
}

func TestACPBindingsNilServiceAndMalformedFilter(t *testing.T) {
	deps := &ACPDeps{Registry: acp.NewACPRegistry()}
	methods := ACPMethods(deps)
	got := decodeProcessPayload[struct {
		Bindings []any `json:"bindings"`
		Count    int   `json:"count"`
	}](t, rpctest.Call(methods, "acp.bindings", nil))
	if got.Bindings == nil || len(got.Bindings) != 0 || got.Count != 0 {
		t.Fatalf("nil binding service response = %#v", got)
	}

	bindings := acp.NewSessionBindingService()
	bindings.Bind(acp.SessionBindParams{Channel: "x", ConversationID: "1", TargetSessionKey: "s"})
	methods = ACPMethods(&ACPDeps{Registry: acp.NewACPRegistry(), Bindings: bindings})
	resp := methods["acp.bindings"](context.Background(), &protocol.RequestFrame{
		ID: "bad", Method: "acp.bindings", Params: json.RawMessage(`{"sessionKey":`),
	})
	all := decodeProcessPayload[map[string]any](t, resp)
	if all["count"] != float64(1) {
		t.Fatalf("malformed bindings fallback = %#v", all)
	}
}

func TestCronAdvancedAndServiceMethodsNilDependencies(t *testing.T) {
	if got := CronAdvancedMethods(CronAdvancedDeps{}); got != nil {
		t.Fatalf("CronAdvancedMethods(nil) = %#v", got)
	}
	if got := CronServiceMethods(CronServiceDeps{}); got != nil {
		t.Fatalf("CronServiceMethods(nil) = %#v", got)
	}
	// Nil broadcast is a no-op and must never panic.
	emitCronChanged(nil, "added", "id")
}

func TestCronAddRejectsInvalidInputAndBroadcastsOnSuccess(t *testing.T) {
	svc := newCronService(t)
	type changed struct{ action, id string }
	var events []changed
	methods := CronAdvancedMethods(CronAdvancedDeps{
		Service: svc,
		Broadcaster: func(name string, payload rtevents.EventPayload) (int, []error) {
			if name != "cron.changed" {
				t.Errorf("event name = %q", name)
			}
			var m map[string]any
			_ = json.Unmarshal(payload.Bytes(), &m)
			events = append(events, changed{action: fmt.Sprint(m["action"]), id: fmt.Sprint(m["id"])})
			return 1, nil
		},
	})

	invalid := []struct {
		name   string
		params map[string]any
		code   string
	}{
		{"missing", map[string]any{}, protocol.ErrMissingParam},
		{"bad schedule", map[string]any{"name": "n", "schedule": "not schedule", "command": "c"}, protocol.ErrValidationFailed},
		{"long command", map[string]any{"name": "n", "schedule": "@hourly", "command": strings.Repeat("x", 4097)}, protocol.ErrValidationFailed},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			resp := rpctest.Call(methods, "cron.add", tc.params)
			rpctest.MustErr(t, resp)
			if resp.Error.Code != tc.code {
				t.Fatalf("code = %q, want %q", resp.Error.Code, tc.code)
			}
		})
	}

	added := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "cron.add", map[string]any{
		"name": "hourly-report", "schedule": "@hourly", "command": "make report", "agentId": "agent-1",
	}))
	if added["id"] != "hourly-report" || added["enabled"] != true || svc.Job("hourly-report") == nil {
		t.Fatalf("default add = %#v job=%#v", added, svc.Job("hourly-report"))
	}
	disabled := false
	added = decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "cron.add", map[string]any{
		"id": "custom-id", "name": "disabled", "schedule": "@daily", "command": "noop", "enabled": disabled,
	}))
	if added["id"] != "custom-id" || added["enabled"] != false || svc.Job("custom-id") == nil || svc.Job("custom-id").Enabled {
		t.Fatalf("disabled add = %#v job=%#v", added, svc.Job("custom-id"))
	}
	if want := []changed{{"added", "hourly-report"}, {"added", "custom-id"}}; !reflect.DeepEqual(events, want) {
		t.Fatalf("cron events = %#v, want %#v", events, want)
	}

	status := decodeProcessPayload[map[string]any](t, rpctest.Call(methods, "cron.status", nil))
	if status["taskCount"] != float64(1) {
		t.Fatalf("cron status = %#v", status)
	}
}

func TestCronRemoveGetListAndRunsFallbacks(t *testing.T) {
	svc := newCronService(t)
	seed := cron.StoreJob{
		ID: "job", Name: "Important Job", Enabled: true,
		Schedule: cron.StoreSchedule{Kind: "every", EveryMs: 60_000},
		Payload:  cron.StorePayload{Kind: "agentTurn", Message: "work"},
	}
	if err := svc.Add(context.Background(), seed); err != nil {
		t.Fatalf("seed Add: %v", err)
	}
	advanced := CronAdvancedMethods(CronAdvancedDeps{Service: svc})
	service := CronServiceMethods(CronServiceDeps{Service: svc})

	job := decodeProcessPayload[cron.StoreJob](t, rpctest.Call(service, "cron.getJob", map[string]any{"jobId": "job"}))
	if job.ID != "job" || job.Name != "Important Job" {
		t.Fatalf("get job = %#v", job)
	}
	for _, params := range []map[string]any{{}, {"id": "missing"}} {
		resp := rpctest.Call(service, "cron.getJob", params)
		rpctest.MustErr(t, resp)
	}

	page := decodeProcessPayload[cron.ListPageResult](t, rpctest.Call(service, "cron.listPage", map[string]any{
		"limit": 10, "offset": -5, "includeDisabled": true, "query": "Important", "sortBy": "name", "sortDir": "asc",
	}))
	if len(page.Jobs) != 1 || page.Jobs[0].ID != "job" {
		t.Fatalf("list page = %#v", page)
	}

	runs := decodeProcessPayload[map[string]any](t, rpctest.Call(advanced, "cron.runs", map[string]any{
		"limit": 99999, "offset": -20, "id": "job",
	}))
	if runs["total"] != float64(0) {
		t.Fatalf("nil runlog response = %#v", runs)
	}
	removed := decodeProcessPayload[map[string]bool](t, rpctest.Call(advanced, "cron.remove", map[string]any{"jobId": "job"}))
	if !removed["removed"] || svc.Job("job") != nil {
		t.Fatalf("remove result = %#v job=%#v", removed, svc.Job("job"))
	}
	removed = decodeProcessPayload[map[string]bool](t, rpctest.Call(advanced, "cron.remove", map[string]any{"id": "missing"}))
	if !removed["removed"] {
		t.Fatalf("idempotent missing remove = %#v", removed)
	}
}
