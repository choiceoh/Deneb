package provider

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestManagedCredential_Expiry(t *testing.T) {
	// Not expired (no expiry set).
	cred := &ManagedCredential{ProviderID: "test", APIKey: "key"}
	if cred.IsExpired() {
		t.Error("expected non-expired with zero ExpiresAt")
	}
	if cred.IsExpiringSoon(5 * time.Minute) {
		t.Error("expected not expiring soon with zero ExpiresAt")
	}

	// Expired.
	cred.ExpiresAt = time.Now().Add(-1 * time.Minute).UnixMilli()
	if !cred.IsExpired() {
		t.Error("expected expired")
	}

	// Expiring soon (within 5 minutes).
	cred.ExpiresAt = time.Now().Add(3 * time.Minute).UnixMilli()
	if cred.IsExpired() {
		t.Error("expected not expired yet")
	}
	if !cred.IsExpiringSoon(5 * time.Minute) {
		t.Error("expected expiring soon within 5 minutes")
	}

	// Not expiring soon (> 5 minutes away).
	cred.ExpiresAt = time.Now().Add(10 * time.Minute).UnixMilli()
	if cred.IsExpiringSoon(5 * time.Minute) {
		t.Error("expected not expiring soon")
	}
}

func TestAuthManager_StoreResolve(t *testing.T) {
	am := NewAuthManager(nil, nil, nil)

	am.Store(&ManagedCredential{
		ProviderID: "openai",
		ProfileID:  "default",
		APIKey:     "sk-123",
		AuthMode:   "api_key",
	})

	cred := am.Resolve("openai", "default")
	if cred == nil {
		t.Fatal("expected credential")
	}
	if cred.APIKey != "sk-123" {
		t.Errorf("expected sk-123, got %q", cred.APIKey)
	}

	// Resolve with normalization.
	am.Store(&ManagedCredential{
		ProviderID: "amazon-bedrock",
		APIKey:     "aws-key",
	})

	cred = am.Resolve("bedrock", "")
	if cred == nil {
		t.Fatal("expected credential for normalized bedrock")
	}
	if cred.APIKey != "aws-key" {
		t.Errorf("expected aws-key, got %q", cred.APIKey)
	}
}

func TestAuthManager_Resolve_NotFound(t *testing.T) {
	am := NewAuthManager(nil, nil, nil)
	cred := am.Resolve("nonexistent", "")
	if cred != nil {
		t.Error("expected nil for unknown provider")
	}
}

// mockForwarder implements Forwarder for testing.
type mockForwarder struct {
	handler func(req *protocol.RequestFrame) (*protocol.ResponseFrame, error)
}

func (m *mockForwarder) Forward(_ context.Context, req *protocol.RequestFrame) (*protocol.ResponseFrame, error) {
	return m.handler(req)
}

func TestAuthManager_Prepare_ViaForwarder(t *testing.T) {
	fwd := &mockForwarder{
		handler: func(req *protocol.RequestFrame) (*protocol.ResponseFrame, error) {
			payload, _ := json.Marshal(PreparedAuth{
				APIKey:    "rotated-key",
				BaseURL:   "https://api.example.com",
				ExpiresAt: time.Now().Add(1 * time.Hour).UnixMilli(),
			})
			return &protocol.ResponseFrame{
				ID:      req.ID,
				OK:      true,
				Payload: payload,
			}, nil
		},
	}

	am := NewAuthManager(nil, fwd, nil)

	prepared, err := am.Prepare(context.Background(), RuntimeAuthContext{
		Provider: "test-provider",
		APIKey:   "old-key",
		AuthMode: "oauth",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.APIKey != "rotated-key" {
		t.Errorf("expected rotated-key, got %q", prepared.APIKey)
	}
	if prepared.BaseURL != "https://api.example.com" {
		t.Errorf("expected base URL, got %q", prepared.BaseURL)
	}

	// Credential should be stored.
	cred := am.Resolve("test-provider", "")
	if cred == nil {
		t.Fatal("expected stored credential after prepare")
	}
	if cred.APIKey != "rotated-key" {
		t.Errorf("expected rotated-key in store, got %q", cred.APIKey)
	}
}

func TestAuthManager_Prepare_NoForwarder(t *testing.T) {
	am := NewAuthManager(nil, nil, nil)

	prepared, err := am.Prepare(context.Background(), RuntimeAuthContext{
		Provider: "test",
		APIKey:   "raw-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.APIKey != "raw-key" {
		t.Errorf("expected raw-key passthrough, got %q", prepared.APIKey)
	}
}

func TestAuthManager_Stop(t *testing.T) {
	am := NewAuthManager(nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	am.StartRotationLoop(ctx)
	// Should not panic on double-stop.
	am.Stop()
	am.Stop()
}
