package provider

import (
	"context"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
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
	am := NewAuthManager(nil, nil)

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
		t.Errorf("got %q, want sk-123", cred.APIKey)
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
		t.Errorf("got %q, want aws-key", cred.APIKey)
	}
}

func TestAuthManager_Resolve_NotFound(t *testing.T) {
	am := NewAuthManager(nil, nil)
	cred := am.Resolve("nonexistent", "")
	if cred != nil {
		t.Error("expected nil for unknown provider")
	}
}

func TestAuthManager_Prepare_NoForwarder(t *testing.T) {
	am := NewAuthManager(nil, nil)

	prepared, err := am.Prepare(context.Background(), RuntimeAuthContext{
		Provider: "test",
		APIKey:   "raw-key",
	})
	testutil.NoError(t, err)
	if prepared.APIKey != "raw-key" {
		t.Errorf("got %q, want raw-key passthrough", prepared.APIKey)
	}
}

func TestAuthManager_Stop(t *testing.T) {
	am := NewAuthManager(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	am.StartRotationLoop(ctx)
	// Should not panic on double-stop.
	am.Stop()
	am.Stop()
}
