package rpcutil

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func quietRPCLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func completeHubConfig() HubConfig {
	return HubConfig{
		Broadcaster:    events.NewBroadcaster(),
		GatewaySubs:    new(events.GatewayEventSubscriptions),
		Sessions:       session.NewManager(),
		Processes:      new(process.Manager),
		JobTracker:     agent.NewJobTracker(quietRPCLogger()),
		CronService:    new(cron.Service),
		CronPersistLog: new(cron.PersistentRunLog),
		Approvals:      approval.NewStore(),
		Skills:         skills.NewRegistry(),
		Logger:         quietRPCLogger(),
		Version:        "v-test",
	}
}

func TestGatewayHubConstructionAccessorsAndOptionalSetters(t *testing.T) {
	cfg := completeHubConfig()
	h := NewGatewayHub(cfg)
	if h == nil || h.Phase() != PhaseInit {
		t.Fatalf("hub/phase = %p/%d", h, h.Phase())
	}
	if h.Broadcaster() != cfg.Broadcaster || h.GatewaySubs() != cfg.GatewaySubs {
		t.Fatal("event accessors changed dependencies")
	}
	if h.Sessions() != cfg.Sessions || h.Processes() != cfg.Processes || h.JobTracker() != cfg.JobTracker {
		t.Fatal("runtime accessors changed dependencies")
	}
	if h.CronService() != cfg.CronService || h.CronPersistLog() != cfg.CronPersistLog {
		t.Fatal("cron accessors changed dependencies")
	}
	if h.Approvals() != cfg.Approvals || h.Skills() != cfg.Skills {
		t.Fatal("workflow accessors changed dependencies")
	}
	if h.Logger() != cfg.Logger || h.Version() != "v-test" || h.Chat() != nil {
		t.Fatalf("metadata/chat = %p/%q/%p", h.Logger(), h.Version(), h.Chat())
	}

	local := new(localai.Hub)
	embed := new(embedding.Client)
	wikiStore := new(wiki.Store)
	contactsStore := new(contacts.Store)
	insightsEngine := new(insights.Engine)
	h.SetLocalAIHub(local)
	h.SetEmbeddingClient(embed)
	h.SetWikiStore(wikiStore)
	h.SetContactsStore(contactsStore)
	h.SetInsights(insightsEngine)
	if h.LocalAIHub() != local || h.EmbeddingClient() != embed || h.WikiStore() != wikiStore || h.ContactsStore() != contactsStore || h.Insights() != insightsEngine {
		t.Fatal("optional setters/accessors did not preserve pointers")
	}
	if err := h.Validate(); err != nil {
		t.Fatalf("complete hub validation: %v", err)
	}
}

func TestGatewayHubValidateReportsAllMissingDependencies(t *testing.T) {
	var nilHub *GatewayHub
	if err := nilHub.Validate(); err == nil || err.Error() != "gatewayHub is nil" {
		t.Fatalf("nil hub validation = %v", err)
	}
	h := NewGatewayHub(HubConfig{})
	err := h.Validate()
	if err == nil {
		t.Fatal("empty hub passed validation")
	}
	wantNames := []string{"Broadcaster", "GatewaySubs", "Sessions", "Processes", "JobTracker", "CronService", "Approvals", "Skills", "Logger"}
	for _, name := range wantNames {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("validation error %q missing %q", err, name)
		}
	}
	for _, optional := range []string{"CronPersistLog", "Chat", "Version", "WikiStore", "ContactsStore", "Insights"} {
		if strings.Contains(err.Error(), optional) {
			t.Errorf("optional dependency %q reported missing: %v", optional, err)
		}
	}
}

func TestGatewayHubValidateEachRequiredDependency(t *testing.T) {
	tests := []struct {
		name string
		drop func(*HubConfig)
	}{
		{name: "Broadcaster", drop: func(c *HubConfig) { c.Broadcaster = nil }},
		{name: "GatewaySubs", drop: func(c *HubConfig) { c.GatewaySubs = nil }},
		{name: "Sessions", drop: func(c *HubConfig) { c.Sessions = nil }},
		{name: "Processes", drop: func(c *HubConfig) { c.Processes = nil }},
		{name: "JobTracker", drop: func(c *HubConfig) { c.JobTracker = nil }},
		{name: "CronService", drop: func(c *HubConfig) { c.CronService = nil }},
		{name: "Approvals", drop: func(c *HubConfig) { c.Approvals = nil }},
		{name: "Skills", drop: func(c *HubConfig) { c.Skills = nil }},
		{name: "Logger", drop: func(c *HubConfig) { c.Logger = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := completeHubConfig()
			tt.drop(&cfg)
			err := NewGatewayHub(cfg).Validate()
			if err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("validation = %v", err)
			}
			for _, other := range tests {
				if other.name != tt.name && strings.Contains(err.Error(), other.name) {
					t.Errorf("validation unexpectedly reports %q: %v", other.name, err)
				}
			}
		})
	}
}

func expectPanicContaining(t *testing.T, substr string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(strings.TrimSpace(toString(r)), substr) {
			t.Fatalf("panic = %#v, want containing %q", r, substr)
		}
	}()
	fn()
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return ""
}

func TestGatewayHubPhaseOrderingAndChatBinding(t *testing.T) {
	h := NewGatewayHub(completeHubConfig())
	chatHandler := new(chat.Handler)
	expectPanicContaining(t, "before PhaseSession", func() { h.SetChat(chatHandler) })
	if h.Chat() != nil || h.Phase() != PhaseInit {
		t.Fatal("failed early SetChat changed hub")
	}
	expectPanicContaining(t, "expected phase 1", func() { h.AdvancePhase(PhaseSession) })
	expectPanicContaining(t, "expected phase 1", func() { h.AdvancePhase(PhaseInit) })
	if h.Phase() != PhaseInit {
		t.Fatalf("invalid transition changed phase to %d", h.Phase())
	}
	h.AdvancePhase(PhaseEarly)
	if h.Phase() != PhaseEarly {
		t.Fatalf("phase = %d", h.Phase())
	}
	expectPanicContaining(t, "before PhaseSession", func() { h.SetChat(chatHandler) })
	h.AdvancePhase(PhaseSession)
	h.SetChat(chatHandler)
	if h.Chat() != chatHandler {
		t.Fatal("chat handler was not bound at session phase")
	}
	h.AdvancePhase(PhaseLate)
	if h.Phase() != PhaseLate {
		t.Fatalf("phase = %d", h.Phase())
	}
	expectPanicContaining(t, "expected phase 4", func() { h.AdvancePhase(PhaseLate + 1) })
	if h.Phase() != PhaseLate {
		t.Fatal("out-of-range transition changed phase")
	}
}

type hubSubscriber struct {
	id       string
	mu       sync.Mutex
	received [][]byte
}

func (s *hubSubscriber) ID() string            { return s.id }
func (s *hubSubscriber) IsAuthenticated() bool { return true }
func (s *hubSubscriber) BufferedAmount() int64 { return 0 }
func (s *hubSubscriber) SendEvent(data []byte) error {
	s.mu.Lock()
	s.received = append(s.received, append([]byte(nil), data...))
	s.mu.Unlock()
	return nil
}

func TestGatewayHubBroadcastAndUnavailableGuard(t *testing.T) {
	var nilHub *GatewayHub
	if sent, errs := nilHub.Broadcast("event", nil); sent != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "not available") {
		t.Fatalf("nil hub broadcast = %d/%v", sent, errs)
	}
	if sent, errs := NewGatewayHub(HubConfig{}).Broadcast("event", nil); sent != 0 || len(errs) != 1 {
		t.Fatalf("missing broadcaster = %d/%v", sent, errs)
	}
	cfg := completeHubConfig()
	sub := &hubSubscriber{id: "conn"}
	cfg.Broadcaster.Subscribe(sub, events.Filter{})
	h := NewGatewayHub(cfg)
	if sent, errs := h.Broadcast("hub.event", map[string]any{"ok": true}); sent != 1 || len(errs) != 0 {
		t.Fatalf("broadcast = %d/%v", sent, errs)
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if len(sub.received) != 1 || !strings.Contains(string(sub.received[0]), `"event":"hub.event"`) {
		t.Fatalf("received = %q", sub.received)
	}
}

func responseErrorCode(resp *protocol.ResponseFrame) string {
	if resp == nil || resp.Error == nil {
		return ""
	}
	return resp.Error.Code
}

func TestUnmarshalAndDecodeParamsBoundaryContracts(t *testing.T) {
	type params struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	for _, raw := range []json.RawMessage{nil, {}, json.RawMessage("")} {
		var out params
		if err := UnmarshalParams(raw, &out); err == nil || err.Error() != "missing params" {
			t.Errorf("UnmarshalParams(%q) = %v", raw, err)
		}
	}
	var out params
	if err := UnmarshalParams(json.RawMessage(`{"name":"alice","n":4}`), &out); err != nil || out.Name != "alice" || out.N != 4 {
		t.Fatalf("unmarshal = %+v/%v", out, err)
	}
	if err := UnmarshalParams(json.RawMessage(`{"name":`), &out); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if err := UnmarshalParams(json.RawMessage(`null`), &out); err != nil {
		t.Fatalf("JSON null should be syntactically valid: %v", err)
	}

	if decoded, resp := DecodeParams[params](nil); resp == nil || responseErrorCode(resp) != protocol.ErrInvalidRequest || decoded != (params{}) || resp.ID != "" {
		t.Fatalf("nil request decode = %+v/%+v", decoded, resp)
	}
	req := &protocol.RequestFrame{ID: "req", Params: json.RawMessage(`{"name":"bob","n":9}`)}
	decoded, resp := DecodeParams[params](req)
	if resp != nil || decoded.Name != "bob" || decoded.N != 9 {
		t.Fatalf("decode = %+v/%+v", decoded, resp)
	}
	bad := &protocol.RequestFrame{ID: "bad", Params: json.RawMessage(`[]`)}
	if _, resp := DecodeParams[params](bad); resp == nil || resp.ID != "bad" || responseErrorCode(resp) != protocol.ErrInvalidRequest {
		t.Fatalf("bad shape response = %+v", resp)
	}
}

func TestTruncateForErrorPreservesUTF8AndByteBudget(t *testing.T) {
	if got := TruncateForError(strings.Repeat("a", MaxKeyInErrorMsg)); len(got) != MaxKeyInErrorMsg || strings.HasSuffix(got, "...") {
		t.Fatalf("exact boundary = %q", got)
	}
	input := strings.Repeat("a", MaxKeyInErrorMsg-1) + "한글"
	got := TruncateForError(input)
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "...") {
		t.Fatalf("UTF-8 truncation = %q valid=%v", got, utf8.ValidString(got))
	}
	if len(strings.TrimSuffix(got, "...")) > MaxKeyInErrorMsg || strings.Contains(got, "�") {
		t.Fatalf("truncated byte budget = %d value=%q", len(got), got)
	}
	if got := TruncateForError("짧은 키"); got != "짧은 키" {
		t.Fatalf("short Unicode changed: %q", got)
	}
}

func TestStandardErrorResponseHelpers(t *testing.T) {
	key, resp := RequireKey("req-key", "  session:one  ")
	if resp != nil || key != "session:one" {
		t.Fatalf("RequireKey valid = %q/%+v", key, resp)
	}
	for _, key := range []string{"", " ", "\n\t"} {
		got, resp := RequireKey("req-key", key)
		if got != "" || resp == nil || resp.ID != "req-key" || responseErrorCode(resp) != protocol.ErrMissingParam {
			t.Errorf("RequireKey(%q) = %q/%+v", key, got, resp)
		}
	}
	missing := ErrMissingKey("missing")
	if missing.ID != "missing" || responseErrorCode(missing) != protocol.ErrMissingParam || !strings.Contains(missing.Error.Message, "key") {
		t.Fatalf("ErrMissingKey = %+v", missing)
	}
	wantErr := errors.New("wrong type")
	invalid := ParamError("invalid", wantErr)
	if invalid.ID != "invalid" || responseErrorCode(invalid) != protocol.ErrInvalidRequest || !strings.Contains(invalid.Error.Message, wantErr.Error()) {
		t.Fatalf("ParamError = %+v", invalid)
	}
}

func TestRespondOKAndMarshalFailureContract(t *testing.T) {
	resp := RespondOK("ok", map[string]any{"value": 7})
	if resp.ID != "ok" || !resp.OK || resp.Error != nil {
		t.Fatalf("success response = %+v", resp)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Payload, &payload); err != nil || payload["value"] != float64(7) {
		t.Fatalf("payload = %#v/%v", payload, err)
	}
	failed := RespondOK("bad", make(chan int))
	if failed.OK || failed.ID != "bad" || responseErrorCode(failed) != protocol.ErrUnavailable {
		t.Fatalf("marshal failure response = %+v", failed)
	}
}

func TestFinalizeSuccessRPCErrorAndOrdinaryError(t *testing.T) {
	success := finalize("success", []string{"a", "b"}, nil)
	if !success.OK || responseErrorCode(success) != "" {
		t.Fatalf("success = %+v", success)
	}
	rpcFailure := rpcerr.NotFound("session").WithSession("session:one")
	resp := finalize("rpc", nil, rpcFailure)
	if resp.OK || responseErrorCode(resp) != protocol.ErrNotFound || !strings.Contains(string(resp.Error.Details), "session:one") {
		t.Fatalf("rpc failure = %+v", resp)
	}
	ordinary := errors.New("domain rejected value")
	resp = finalize("ordinary", nil, ordinary)
	if resp.OK || responseErrorCode(resp) != protocol.ErrInvalidRequest || !strings.Contains(resp.Error.Message, ordinary.Error()) {
		t.Fatalf("ordinary failure = %+v", resp)
	}
}

type bindParams struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

func bindRequest(id, raw string) *protocol.RequestFrame {
	return &protocol.RequestFrame{ID: id, Params: json.RawMessage(raw)}
}

func TestBindSuccessAndErrorPaths(t *testing.T) {
	req := bindRequest("bind", `{"name":"alice","n":3}`)
	called := 0
	resp := Bind[bindParams](req, func(p bindParams) (any, error) {
		called++
		return map[string]any{"greeting": p.Name, "double": p.N * 2}, nil
	})
	if !resp.OK || called != 1 {
		t.Fatalf("Bind success = %+v calls=%d", resp, called)
	}
	var payload map[string]any
	_ = json.Unmarshal(resp.Payload, &payload)
	if payload["greeting"] != "alice" || payload["double"] != float64(6) {
		t.Fatalf("Bind payload = %#v", payload)
	}

	bad := Bind[bindParams](bindRequest("bad", `{`), func(bindParams) (any, error) {
		called++
		return nil, nil
	})
	if bad.OK || responseErrorCode(bad) != protocol.ErrInvalidRequest || called != 1 {
		t.Fatalf("decode failure = %+v calls=%d", bad, called)
	}
	rpcFailure := Bind[bindParams](req, func(bindParams) (any, error) {
		return nil, rpcerr.Conflict("busy")
	})
	if responseErrorCode(rpcFailure) != protocol.ErrConflict {
		t.Fatalf("RPC failure = %+v", rpcFailure)
	}
	ordinary := Bind[bindParams](req, func(bindParams) (any, error) {
		return nil, errors.New("nope")
	})
	if responseErrorCode(ordinary) != protocol.ErrInvalidRequest {
		t.Fatalf("ordinary failure = %+v", ordinary)
	}
}

func TestBindCtxForwardsContextAndCancellation(t *testing.T) {
	type contextKey string
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey("key"), "value"))
	cancel()
	req := bindRequest("ctx", `{"name":"bob","n":2}`)
	resp := BindCtx[bindParams](ctx, req, func(got context.Context, p bindParams) (any, error) {
		if got.Value(contextKey("key")) != "value" || !errors.Is(got.Err(), context.Canceled) {
			t.Fatalf("context = value=%v err=%v", got.Value(contextKey("key")), got.Err())
		}
		return p.Name, nil
	})
	if !resp.OK {
		t.Fatalf("BindCtx = %+v", resp)
	}
	var name string
	if err := json.Unmarshal(resp.Payload, &name); err != nil || name != "bob" {
		t.Fatalf("name = %q/%v", name, err)
	}

	called := false
	bad := BindCtx[bindParams](ctx, bindRequest("bad", ""), func(context.Context, bindParams) (any, error) {
		called = true
		return nil, nil
	})
	if bad.OK || called {
		t.Fatalf("invalid BindCtx = %+v called=%v", bad, called)
	}
}

func TestBoundHandlerContracts(t *testing.T) {
	handler := BindHandler[bindParams](func(p bindParams) (any, error) {
		return p.Name + "!", nil
	})
	resp := handler(context.Background(), bindRequest("handler", `{"name":"hello"}`))
	if !resp.OK {
		t.Fatalf("handler = %+v", resp)
	}
	var text string
	_ = json.Unmarshal(resp.Payload, &text)
	if text != "hello!" {
		t.Fatalf("handler result = %q", text)
	}

	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("key"), "ctx")
	ctxHandler := BindHandlerCtx[bindParams](func(got context.Context, p bindParams) (any, error) {
		return []string{got.Value(contextKey("key")).(string), p.Name}, nil
	})
	resp = ctxHandler(ctx, bindRequest("ctx-handler", `{"name":"payload"}`))
	var values []string
	_ = json.Unmarshal(resp.Payload, &values)
	if !reflect.DeepEqual(values, []string{"ctx", "payload"}) {
		t.Fatalf("context handler values = %#v", values)
	}
}

func TestNilBoundFunctionsReturnErrorsInsteadOfPanicking(t *testing.T) {
	req := bindRequest("nil", `{"name":"x"}`)
	responses := []*protocol.ResponseFrame{
		Bind[bindParams](req, nil),
		Bind[bindParams](nil, nil),
		BindCtx[bindParams](context.Background(), req, nil),
		BindCtx[bindParams](context.Background(), nil, nil),
		BindHandler[bindParams](nil)(context.Background(), req),
		BindHandler[bindParams](nil)(context.Background(), nil),
		BindHandlerCtx[bindParams](nil)(context.Background(), req),
		BindHandlerCtx[bindParams](nil)(context.Background(), nil),
	}
	for i, resp := range responses {
		if resp == nil || resp.OK || responseErrorCode(resp) != protocol.ErrInvalidRequest || !strings.Contains(resp.Error.Message, "handler is nil") {
			t.Errorf("response %d = %+v", i, resp)
		}
	}
	if responses[0].ID != "nil" || responses[1].ID != "" {
		t.Fatalf("nil handler request IDs = %q/%q", responses[0].ID, responses[1].ID)
	}
}

func TestRequestIDHelperContract(t *testing.T) {
	if requestID(nil) != "" || requestID(&protocol.RequestFrame{ID: "id"}) != "id" {
		t.Fatal("requestID helper mismatch")
	}
}
