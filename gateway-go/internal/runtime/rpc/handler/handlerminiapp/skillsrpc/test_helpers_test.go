package skillsrpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func authedSkillsCtx() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{})
}

func decodeSkillsPayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	if resp == nil || !resp.OK {
		t.Fatalf("expected OK response, got %+v", resp)
	}
	var out T
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return out
}

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
