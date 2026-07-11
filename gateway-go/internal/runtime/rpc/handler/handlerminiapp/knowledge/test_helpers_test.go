package knowledge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func authedCtx() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{
		User: &clientauth.User{ID: 42, FirstName: "Tester"},
	})
}

func reqWith(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("t-1", method, params)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return req
}

func newReq(t *testing.T, method string) *protocol.RequestFrame {
	t.Helper()
	return reqWith(t, method, nil)
}

func decodePayload(t *testing.T, resp *protocol.ResponseFrame) map[string]any {
	t.Helper()
	var got map[string]any
	decode(t, resp, &got)
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

func decode(t *testing.T, resp *protocol.ResponseFrame, dest any) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if !resp.OK {
		t.Fatalf("response not OK: code=%s message=%s", resp.Error.Code, resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Payload, dest); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, string(resp.Payload))
	}
}
