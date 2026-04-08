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
		t.Fatal("non-public method should require auth")
	}
	if err.Code != protocol.ErrUnauthorized {
		t.Errorf("got %q, want UNAUTHORIZED", err.Code)
	}
}

func TestAuthorizeOperatorAllowed(t *testing.T) {
	err := AuthorizeMethod("sessions.list", "operator", true)
	if err != nil {
		t.Errorf("authenticated operator should be authorized, got: %+v", err)
	}
}

func TestAuthorizeAgentAllowed(t *testing.T) {
	err := AuthorizeMethod("sessions.list", "agent", true)
	if err != nil {
		t.Errorf("authenticated agent should be authorized, got: %+v", err)
	}
}

func TestAuthorizeProbeBlocked(t *testing.T) {
	err := AuthorizeMethod("sessions.list", "probe", true)
	if err == nil {
		t.Fatal("probe should not access non-public methods")
	}
	if err.Code != protocol.ErrForbidden {
		t.Errorf("got %q, want FORBIDDEN", err.Code)
	}
}

func TestAuthorizePublicMethodForProbe(t *testing.T) {
	err := AuthorizeMethod("health", "probe", true)
	if err != nil {
		t.Errorf("probe should access public methods, got: %+v", err)
	}
}
