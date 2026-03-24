package auth

import (
	"testing"
	"time"
)

func TestIssueAndValidateToken(t *testing.T) {
	v := NewValidator([]byte("test-secret"))

	token, err := v.IssueToken("device-1", RoleOperator, []Scope{ScopeRead, ScopeWrite})
	if err != nil {
		t.Fatalf("issue error: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if claims.DeviceID != "device-1" {
		t.Errorf("expected device-1, got %s", claims.DeviceID)
	}
	if claims.Role != RoleOperator {
		t.Errorf("expected operator, got %s", claims.Role)
	}
	if len(claims.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(claims.Scopes))
	}
}

func TestIssueToken_EmptyDeviceID(t *testing.T) {
	v := NewValidator([]byte("secret"))
	_, err := v.IssueToken("", RoleOperator, nil)
	if err == nil {
		t.Error("expected error for empty device ID")
	}
}

func TestIssueToken_DeviceIDWithColon(t *testing.T) {
	v := NewValidator([]byte("secret"))
	_, err := v.IssueToken("device:bad", RoleOperator, nil)
	if err == nil {
		t.Error("expected error for device ID containing colon")
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	v := NewValidator([]byte("test-secret"))
	// 64 hex chars (all zeros) + colon + payload
	_, err := v.ValidateToken("0000000000000000000000000000000000000000000000000000000000000000:device-1:operator:read:12345")
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestValidateToken_InvalidFormat(t *testing.T) {
	v := NewValidator([]byte("test-secret"))
	_, err := v.ValidateToken("not-a-token")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestValidateToken_TooShort(t *testing.T) {
	v := NewValidator([]byte("test-secret"))
	_, err := v.ValidateToken("abcd:payload")
	if err == nil {
		t.Fatal("expected error for short token")
	}
}

func TestValidateToken_DifferentSecret(t *testing.T) {
	v1 := NewValidator([]byte("secret-1"))
	v2 := NewValidator([]byte("secret-2"))

	token, _ := v1.IssueToken("dev", RoleViewer, []Scope{ScopeRead})
	_, err := v2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for different secret")
	}
}

func TestTokenClaims_IsExpired(t *testing.T) {
	claims := &TokenClaims{
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if !claims.IsExpired(time.Now()) {
		t.Error("expected expired")
	}

	claims.ExpiresAt = time.Now().Add(1 * time.Hour)
	if claims.IsExpired(time.Now()) {
		t.Error("expected not expired")
	}

	claims.ExpiresAt = time.Time{}
	if claims.IsExpired(time.Now()) {
		t.Error("expected not expired for zero time")
	}
}

func TestTokenClaims_HasScope(t *testing.T) {
	claims := &TokenClaims{
		Scopes: []Scope{ScopeRead, ScopeWrite},
	}
	if !claims.HasScope(ScopeRead) {
		t.Error("expected HasScope(read) = true")
	}
	if claims.HasScope(ScopeAdmin) {
		t.Error("expected HasScope(admin) = false")
	}
}

func TestDeviceManagement(t *testing.T) {
	v := NewValidator([]byte("secret"))

	dev := DeviceRecord{
		ID:       "dev-1",
		Name:     "Test Device",
		PairedAt: time.Now(),
		LastSeen: time.Now(),
	}
	v.RegisterDevice(dev)

	got := v.GetDevice("dev-1")
	if got == nil {
		t.Fatal("expected device")
	}
	if got.Name != "Test Device" {
		t.Errorf("expected 'Test Device', got %s", got.Name)
	}

	devices := v.ListDevices()
	if len(devices) != 1 {
		t.Errorf("expected 1 device, got %d", len(devices))
	}
	// ListDevices returns copies, so mutating them is safe.
	if devices[0].Name != "Test Device" {
		t.Errorf("expected copy with name 'Test Device', got %s", devices[0].Name)
	}

	v.TouchDevice("dev-1")

	removed := v.RemoveDevice("dev-1")
	if !removed {
		t.Error("expected removal to return true")
	}

	if v.GetDevice("dev-1") != nil {
		t.Error("expected nil after removal")
	}
}

func TestCheckPermission(t *testing.T) {
	// Operator has all scopes.
	if err := CheckPermission(RoleOperator, nil, ScopeWrite); err != nil {
		t.Errorf("operator should have write: %v", err)
	}

	// Viewer lacks write.
	if err := CheckPermission(RoleViewer, nil, ScopeWrite); err == nil {
		t.Error("viewer should not have write")
	}

	// Explicit scope override.
	if err := CheckPermission(RoleViewer, []Scope{ScopeWrite}, ScopeWrite); err != nil {
		t.Errorf("explicit scope should grant access: %v", err)
	}

	// Admin scope grants everything.
	if err := CheckPermission(RoleViewer, []Scope{ScopeAdmin}, ScopeExecute); err != nil {
		t.Errorf("admin scope should grant execute: %v", err)
	}
}
