package rpcutil

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

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
	type responsePayload struct {
		Value int `json:"value"`
	}
	resp := RespondOK("ok", responsePayload{Value: 7})
	if resp.ID != "ok" || !resp.OK || resp.Error != nil {
		t.Fatalf("success response = %+v", resp)
	}
	var payload responsePayload
	if err := json.Unmarshal(resp.Payload, &payload); err != nil || payload.Value != 7 {
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
	resp := finalize("rpc", struct{}{}, rpcFailure)
	if resp.OK || responseErrorCode(resp) != protocol.ErrNotFound || !strings.Contains(string(resp.Error.Details), "session:one") {
		t.Fatalf("rpc failure = %+v", resp)
	}
	ordinary := errors.New("domain rejected value")
	resp = finalize("ordinary", struct{}{}, ordinary)
	if resp.OK || responseErrorCode(resp) != protocol.ErrInvalidRequest || !strings.Contains(resp.Error.Message, ordinary.Error()) {
		t.Fatalf("ordinary failure = %+v", resp)
	}
}

type bindParams struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

type bindResult struct {
	Greeting string `json:"greeting"`
	Double   int    `json:"double"`
}

func bindRequest(id, raw string) *protocol.RequestFrame {
	return &protocol.RequestFrame{ID: id, Params: json.RawMessage(raw)}
}

func TestBindSuccessAndErrorPaths(t *testing.T) {
	req := bindRequest("bind", `{"name":"alice","n":3}`)
	called := 0
	resp := Bind[bindParams](req, func(p bindParams) (bindResult, error) {
		called++
		return bindResult{Greeting: p.Name, Double: p.N * 2}, nil
	})
	if !resp.OK || called != 1 {
		t.Fatalf("Bind success = %+v calls=%d", resp, called)
	}
	var payload bindResult
	_ = json.Unmarshal(resp.Payload, &payload)
	if payload.Greeting != "alice" || payload.Double != 6 {
		t.Fatalf("Bind payload = %#v", payload)
	}

	bad := Bind[bindParams](bindRequest("bad", `{`), func(bindParams) (bindResult, error) {
		called++
		return bindResult{}, nil
	})
	if bad.OK || responseErrorCode(bad) != protocol.ErrInvalidRequest || called != 1 {
		t.Fatalf("decode failure = %+v calls=%d", bad, called)
	}
	rpcFailure := Bind[bindParams](req, func(bindParams) (bindResult, error) {
		return bindResult{}, rpcerr.Conflict("busy")
	})
	if responseErrorCode(rpcFailure) != protocol.ErrConflict {
		t.Fatalf("RPC failure = %+v", rpcFailure)
	}
	ordinary := Bind[bindParams](req, func(bindParams) (bindResult, error) {
		return bindResult{}, errors.New("nope")
	})
	if responseErrorCode(ordinary) != protocol.ErrInvalidRequest {
		t.Fatalf("ordinary failure = %+v", ordinary)
	}
}

func TestGenericBindPreservesResponseJSONAndErrorShapes(t *testing.T) {
	req := bindRequest("shape", `{"name":"alice","n":3}`)
	success := Bind[bindParams](req, func(p bindParams) (bindResult, error) {
		return bindResult{Greeting: p.Name, Double: p.N * 2}, nil
	})
	requireResponseJSON(t, success, `{"type":"res","id":"shape","ok":true,"payload":{"greeting":"alice","double":6}}`)

	conflict := Bind[bindParams](req, func(bindParams) (bindResult, error) {
		return bindResult{}, rpcerr.Conflict("busy")
	})
	requireResponseJSON(t, conflict, `{"type":"res","id":"shape","ok":false,"error":{"code":"CONFLICT","message":"busy"}}`)

	ordinary := Bind[bindParams](req, func(bindParams) (bindResult, error) {
		return bindResult{}, errors.New("nope")
	})
	requireResponseJSON(t, ordinary, `{"type":"res","id":"shape","ok":false,"error":{"code":"INVALID_REQUEST","message":"invalid params: nope"}}`)
}

func requireResponseJSON(t *testing.T, response *protocol.ResponseFrame, want string) {
	t.Helper()
	got, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if string(got) != want {
		t.Fatalf("response JSON changed\n got: %s\nwant: %s", got, want)
	}
}

func TestBindCtxForwardsContextAndCancellation(t *testing.T) {
	type contextKey string
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey("key"), "value"))
	cancel()
	req := bindRequest("ctx", `{"name":"bob","n":2}`)
	resp := BindCtx[bindParams](ctx, req, func(got context.Context, p bindParams) (string, error) {
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
	bad := BindCtx[bindParams](ctx, bindRequest("bad", ""), func(context.Context, bindParams) (string, error) {
		called = true
		return "", nil
	})
	if bad.OK || called {
		t.Fatalf("invalid BindCtx = %+v called=%v", bad, called)
	}
}

func TestBoundHandlerContracts(t *testing.T) {
	handler := BindHandler[bindParams](func(p bindParams) (string, error) {
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
	ctxHandler := BindHandlerCtx[bindParams](func(got context.Context, p bindParams) ([]string, error) {
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
		Bind[bindParams, bindResult](req, nil),
		Bind[bindParams, bindResult](nil, nil),
		BindCtx[bindParams, bindResult](context.Background(), req, nil),
		BindCtx[bindParams, bindResult](context.Background(), nil, nil),
		BindHandler[bindParams, bindResult](nil)(context.Background(), req),
		BindHandler[bindParams, bindResult](nil)(context.Background(), nil),
		BindHandlerCtx[bindParams, bindResult](nil)(context.Background(), req),
		BindHandlerCtx[bindParams, bindResult](nil)(context.Background(), nil),
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
