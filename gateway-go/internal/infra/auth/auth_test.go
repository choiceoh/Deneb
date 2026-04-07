package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

// issueTestToken creates a signed token for testing.
func issueTestToken(secret []byte, deviceID string, role Role) string {
	now := time.Now().Unix()
	payload := fmt.Sprintf("%s:%s::%d", deviceID, role, now)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return sig + ":" + payload
}

func TestValidateToken(t *testing.T) {
	secret := []byte("test-secret")
	v := NewValidator(secret)

	token := issueTestToken(secret, "device-1", RoleOperator)
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
}

func TestValidateToken_AgentRole(t *testing.T) {
	secret := []byte("test-secret")
	v := NewValidator(secret)

	token := issueTestToken(secret, "agent-1", RoleAgent)
	claims, err := v.ValidateToken(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.Role != RoleAgent {
		t.Errorf("expected agent, got %s", claims.Role)
	}
}

func TestValidateToken_InvalidSignature(t *testing.T) {
	v := NewValidator([]byte("test-secret"))
	_, err := v.ValidateToken("0000000000000000000000000000000000000000000000000000000000000000:device-1:operator::12345")
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
	secret1 := []byte("secret-1")
	secret2 := []byte("secret-2")
	v2 := NewValidator(secret2)

	token := issueTestToken(secret1, "dev", RoleOperator)
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
