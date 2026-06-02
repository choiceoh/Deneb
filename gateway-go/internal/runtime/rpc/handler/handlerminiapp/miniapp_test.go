package handlerminiapp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func newReq(t *testing.T, method string) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("test-1", method, nil)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return req
}

func decodePayload(t *testing.T, resp *protocol.ResponseFrame) map[string]any {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if !resp.OK {
		t.Fatalf("response not OK: code=%s message=%s", resp.Error.Code, resp.Error.Message)
	}
	var got map[string]any
	if err := json.Unmarshal(resp.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, string(resp.Payload))
	}
	return got
}

func sampleIdentity() *clientauth.Identity {
	return &clientauth.Identity{
		User: &clientauth.User{
			ID:           42,
			FirstName:    "오선택",
			Username:     "choiceoh",
			LanguageCode: "ko",
			IsPremium:    true,
		},
		AuthDate: time.Unix(1_700_000_000, 0).UTC(),
	}
}

func TestPing_WithIdentity(t *testing.T) {
	h := ping(Deps{Version: "4.22.3"})
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	resp := h(ctx, newReq(t, "miniapp.ping"))
	got := decodePayload(t, resp)

	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["version"] != "4.22.3" {
		t.Errorf("version = %v, want 4.22.3", got["version"])
	}
	if _, ok := got["tsMs"].(float64); !ok {
		t.Errorf("tsMs missing or not numeric: %#v", got["tsMs"])
	}
}

func TestPing_IncludesModelWhenResolvable(t *testing.T) {
	h := ping(Deps{
		Version:      "4.22.3",
		CurrentModel: func() string { return "vllm/gemma4" },
	})
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := decodePayload(t, h(ctx, newReq(t, "miniapp.ping")))
	if got["model"] != "vllm/gemma4" {
		t.Errorf("model = %v, want vllm/gemma4", got["model"])
	}
}

func TestPing_OmitsModelWhenUnresolved(t *testing.T) {
	cases := []struct {
		name string
		deps Deps
	}{
		{"nil accessor", Deps{Version: "x"}},
		{"empty result", Deps{Version: "x", CurrentModel: func() string { return "" }}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := ping(tc.deps)
			ctx := clientauth.WithContext(context.Background(), sampleIdentity())
			got := decodePayload(t, h(ctx, newReq(t, "miniapp.ping")))
			if _, ok := got["model"]; ok {
				t.Errorf("model key present in payload, want absent: %#v", got)
			}
		})
	}
}

func TestPing_NoIdentity(t *testing.T) {
	h := ping(Deps{Version: "4.22.3"})
	resp := h(context.Background(), newReq(t, "miniapp.ping"))

	if resp.OK {
		t.Fatalf("expected error response, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestClientHello_WithCapabilities(t *testing.T) {
	h := clientHello(Deps{
		Version:      "4.22.3",
		CurrentModel: func() string { return "vllm/gemma4" },
		Capabilities: func() map[string]bool {
			return map[string]bool{
				"chatStream": true,
				"gmail":      false,
			}
		},
	})
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := decodePayload(t, h(ctx, newReq(t, "miniapp.client.hello")))
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
	if got["version"] != "4.22.3" {
		t.Errorf("version = %v, want 4.22.3", got["version"])
	}
	if got["model"] != "vllm/gemma4" {
		t.Errorf("model = %v, want vllm/gemma4", got["model"])
	}
	if got["nativeApiVersion"] != float64(1) {
		t.Errorf("nativeApiVersion = %v, want 1", got["nativeApiVersion"])
	}
	caps, ok := got["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities missing or wrong type: %#v", got["capabilities"])
	}
	if caps["rpc"] != true {
		t.Errorf("rpc capability = %v, want true", caps["rpc"])
	}
	if caps["chatStream"] != true {
		t.Errorf("chatStream capability = %v, want true", caps["chatStream"])
	}
	if caps["gmail"] != false {
		t.Errorf("gmail capability = %v, want false", caps["gmail"])
	}
	endpoints, ok := got["endpoints"].(map[string]any)
	if !ok {
		t.Fatalf("endpoints missing or wrong type: %#v", got["endpoints"])
	}
	if endpoints["rpc"] != "/api/v1/miniapp/rpc" {
		t.Errorf("rpc endpoint = %v", endpoints["rpc"])
	}
}

func TestClientHello_NoIdentity(t *testing.T) {
	h := clientHello(Deps{Version: "4.22.3"})
	resp := h(context.Background(), newReq(t, "miniapp.client.hello"))

	if resp.OK {
		t.Fatalf("expected error response, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestWhoami_WithIdentity(t *testing.T) {
	h := whoami()
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	resp := h(ctx, newReq(t, "miniapp.whoami"))
	got := decodePayload(t, resp)

	if id, _ := got["id"].(float64); int64(id) != 42 {
		t.Errorf("id = %v, want 42", got["id"])
	}
	if got["firstName"] != "오선택" {
		t.Errorf("firstName = %v, want 오선택", got["firstName"])
	}
	if got["username"] != "choiceoh" {
		t.Errorf("username = %v, want choiceoh", got["username"])
	}
	if got["isPremium"] != true {
		t.Errorf("isPremium = %v, want true", got["isPremium"])
	}
}

func TestWhoami_NoIdentity(t *testing.T) {
	h := whoami()
	resp := h(context.Background(), newReq(t, "miniapp.whoami"))

	if resp.OK {
		t.Fatalf("expected error response, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestWhoami_NoUser(t *testing.T) {
	data := sampleIdentity()
	data.User = nil
	h := whoami()
	ctx := clientauth.WithContext(context.Background(), data)

	resp := h(ctx, newReq(t, "miniapp.whoami"))
	if resp.OK {
		t.Fatalf("expected error response, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestMethods_RegistersCoreMethods(t *testing.T) {
	got := Methods(Deps{Version: "x"})
	for _, name := range []string{"miniapp.ping", "miniapp.whoami", "miniapp.client.hello"} {
		if _, ok := got[name]; !ok {
			t.Errorf("Methods() missing %q", name)
		}
	}
	if len(got) != 3 {
		t.Errorf("len(Methods()) = %d, want 3", len(got))
	}
}
