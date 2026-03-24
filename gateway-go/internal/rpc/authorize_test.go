package rpc

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestAuthorizePublicMethod(t *testing.T) {
	err := AuthorizeMethod("health", "", false)
	if err != nil {
		t.Errorf("health should be public, got error: %+v", err)
	}
}

func TestAuthorizeRequiresAuth(t *testing.T) {
	err := AuthorizeMethod("sessions.list", "", false)
	if err == nil {
		t.Error("non-public method should require auth")
	}
	if err.Code != protocol.ErrUnauthorized {
		t.Errorf("expected UNAUTHORIZED, got %q", err.Code)
	}
}

func TestAuthorizeWithRole(t *testing.T) {
	err := AuthorizeMethod("sessions.list", "operator", true)
	if err != nil {
		t.Errorf("authenticated operator should be authorized, got: %+v", err)
	}
}
