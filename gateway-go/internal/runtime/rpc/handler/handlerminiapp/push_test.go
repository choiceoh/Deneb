package handlerminiapp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakePushStore struct {
	registered []string
	platforms  []string
	removed    []string
	count      int
}

func (s *fakePushStore) Register(token, platform string) (int, error) {
	s.registered = append(s.registered, token)
	s.platforms = append(s.platforms, platform)
	s.count++
	return s.count, nil
}

func (s *fakePushStore) Unregister(token string) (int, error) {
	s.removed = append(s.removed, token)
	if s.count > 0 {
		s.count--
	}
	return s.count, nil
}

func newPushReq(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("test-1", method, params)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return req
}

func TestPushMethods_NilStoreUnregistered(t *testing.T) {
	if m := PushMethods(PushDeps{}); m != nil {
		t.Errorf("expected nil method map without a store, got %d methods", len(m))
	}
}

func TestPushRegister_WithIdentity(t *testing.T) {
	store := &fakePushStore{}
	h := pushRegister(PushDeps{Store: store})
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := decodePayload(t, h(ctx, newPushReq(t, "miniapp.push.register",
		map[string]any{"token": "fcm-token-abc", "platform": "android"})))
	if got["ok"] != true {
		t.Errorf("ok = %v", got["ok"])
	}
	if len(store.registered) != 1 || store.registered[0] != "fcm-token-abc" {
		t.Errorf("registered = %v", store.registered)
	}
	if store.platforms[0] != "android" {
		t.Errorf("platform = %v", store.platforms[0])
	}
}

func TestPushRegister_NoIdentityRejected(t *testing.T) {
	store := &fakePushStore{}
	h := pushRegister(PushDeps{Store: store})
	resp := h(context.Background(), newPushReq(t, "miniapp.push.register",
		map[string]any{"token": "x"}))
	if resp.OK {
		t.Fatal("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
	if len(store.registered) != 0 {
		t.Error("store must not be touched without identity")
	}
}

func TestPushRegister_MissingToken(t *testing.T) {
	h := pushRegister(PushDeps{Store: &fakePushStore{}})
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())
	resp := h(ctx, newPushReq(t, "miniapp.push.register", map[string]any{"platform": "android"}))
	if resp.OK {
		t.Fatal("expected error for missing token")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestPushUnregister_WithIdentity(t *testing.T) {
	store := &fakePushStore{count: 2}
	h := pushUnregister(PushDeps{Store: store})
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := decodePayload(t, h(ctx, newPushReq(t, "miniapp.push.unregister",
		map[string]any{"token": "fcm-token-abc"})))
	if got["ok"] != true {
		t.Errorf("ok = %v", got["ok"])
	}
	if len(store.removed) != 1 || store.removed[0] != "fcm-token-abc" {
		t.Errorf("removed = %v", store.removed)
	}
}
