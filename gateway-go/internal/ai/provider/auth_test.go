package provider

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// fakeAuthProvider is a provider plugin that implements RuntimeAuthProvider so
// AuthManager.Prepare delegates to it. It records each call and returns a
// rotated key, standing in for a real OAuth/token refresh forwarder.
type fakeAuthProvider struct {
	id           string
	newKey       string
	newExpiresAt int64

	mu      sync.Mutex
	calls   int
	lastReq RuntimeAuthContext
}

func (f *fakeAuthProvider) ID() string                { return f.id }
func (f *fakeAuthProvider) Label() string             { return "Fake" }
func (f *fakeAuthProvider) AuthMethods() []AuthMethod { return nil }

func (f *fakeAuthProvider) PrepareRuntimeAuth(_ context.Context, cctx RuntimeAuthContext) (*PreparedAuth, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastReq = cctx
	return &PreparedAuth{APIKey: f.newKey, ExpiresAt: f.newExpiresAt}, nil
}

func (f *fakeAuthProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

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

func TestAuthManager_RefreshIfNeeded_RotatesExpiringCredential(t *testing.T) {
	fp := &fakeAuthProvider{
		id:           "fakeauth",
		newKey:       "new-key",
		newExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	reg := NewRegistry()
	if err := reg.Register(fp); err != nil {
		t.Fatalf("register: %v", err)
	}
	am := NewAuthManager(reg, nil)
	am.Store(&ManagedCredential{
		ProviderID: "fakeauth",
		APIKey:     "old-key",
		ExpiresAt:  time.Now().Add(3 * time.Minute).UnixMilli(), // inside the 5m refresh window
	})

	if err := am.RefreshIfNeeded(context.Background(), "fakeauth", ""); err != nil {
		t.Fatalf("RefreshIfNeeded: %v", err)
	}

	if fp.callCount() != 1 {
		t.Fatalf("forwarder calls = %d, want 1", fp.callCount())
	}
	got := am.Resolve("fakeauth", "")
	if got == nil || got.APIKey != "new-key" {
		t.Fatalf("APIKey = %v, want new-key after rotation", got)
	}
	if got.RefreshedAt == 0 {
		t.Error("RefreshedAt not stamped after rotation")
	}
	if got.IsExpiringSoon(5 * time.Minute) {
		t.Error("credential still expiring soon after rotation to a 1h expiry")
	}
}

func TestAuthManager_RefreshIfNeeded_NoopWhenNotExpiringOrMissing(t *testing.T) {
	fp := &fakeAuthProvider{id: "fakeauth", newKey: "new-key"}
	reg := NewRegistry()
	if err := reg.Register(fp); err != nil {
		t.Fatalf("register: %v", err)
	}
	am := NewAuthManager(reg, nil)

	// Missing credential: nothing to refresh, forwarder untouched.
	if err := am.RefreshIfNeeded(context.Background(), "ghost", ""); err != nil {
		t.Fatalf("RefreshIfNeeded(missing): %v", err)
	}

	// Far-future credential: not expiring soon, so no refresh.
	am.Store(&ManagedCredential{
		ProviderID: "fakeauth",
		APIKey:     "stable-key",
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	})
	if err := am.RefreshIfNeeded(context.Background(), "fakeauth", ""); err != nil {
		t.Fatalf("RefreshIfNeeded(fresh): %v", err)
	}

	if fp.callCount() != 0 {
		t.Fatalf("forwarder calls = %d, want 0 (no expiring credential)", fp.callCount())
	}
	if got := am.Resolve("fakeauth", ""); got == nil || got.APIKey != "stable-key" {
		t.Fatalf("APIKey = %v, want unchanged stable-key", got)
	}
}

// TestAuthManager_RefreshExpiring_SelectsOnlyExpiring is the selection-logic
// guard: refreshExpiring must refresh exactly the credentials inside the 5m
// window and leave far-future / no-expiry credentials untouched.
func TestAuthManager_RefreshExpiring_SelectsOnlyExpiring(t *testing.T) {
	fp := &fakeAuthProvider{
		id:           "expiring",
		newKey:       "rotated",
		newExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
	}
	reg := NewRegistry()
	if err := reg.Register(fp); err != nil {
		t.Fatalf("register: %v", err)
	}
	am := NewAuthManager(reg, nil)

	now := time.Now()
	am.Store(&ManagedCredential{ProviderID: "expiring", APIKey: "old", ExpiresAt: now.Add(2 * time.Minute).UnixMilli()})
	am.Store(&ManagedCredential{ProviderID: "farfuture", APIKey: "keep", ExpiresAt: now.Add(time.Hour).UnixMilli()})
	am.Store(&ManagedCredential{ProviderID: "noexpiry", APIKey: "keep"}) // ExpiresAt 0 → never expires

	am.refreshExpiring(context.Background())

	if fp.callCount() != 1 {
		t.Fatalf("forwarder calls = %d, want 1 (only the expiring credential)", fp.callCount())
	}
	if fp.lastReq.Provider != "expiring" {
		t.Errorf("refreshed provider = %q, want expiring", fp.lastReq.Provider)
	}
	if got := am.Resolve("expiring", ""); got == nil || got.APIKey != "rotated" {
		t.Errorf("expiring APIKey = %v, want rotated", got)
	}
	if got := am.Resolve("farfuture", ""); got == nil || got.APIKey != "keep" {
		t.Errorf("farfuture APIKey = %v, want untouched keep", got)
	}
	if got := am.Resolve("noexpiry", ""); got == nil || got.APIKey != "keep" {
		t.Errorf("noexpiry APIKey = %v, want untouched keep", got)
	}
}

// TestAuthManager_RotationLoop_StartStopSafe exercises the loop lifecycle:
// Stop is idempotent (sync.Once), and both explicit Stop and ctx cancellation
// unwind the background goroutine without panic or deadlock (run under -race).
func TestAuthManager_RotationLoop_StartStopSafe(t *testing.T) {
	am := NewAuthManager(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	am.StartRotationLoop(ctx)
	am.Stop()
	am.Stop() // idempotent: second close must not panic

	// A manager whose context is cancelled must also unwind cleanly.
	am2 := NewAuthManager(nil, nil)
	ctx2, cancel2 := context.WithCancel(context.Background())
	am2.StartRotationLoop(ctx2)
	cancel2()
	am2.Stop()
}
