package rpc

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestAuthorizePublicMethod(t *testing.T) {
	err := AuthorizeMethod("health", "", false, nil)
	if err != nil {
		t.Errorf("health should be public, got error: %+v", err)
	}
}

func TestAuthorizeRequiresAuth(t *testing.T) {
	err := AuthorizeMethod("sessions.list", "", false, nil)
	if err == nil {
		t.Error("non-public method should require auth")
	}
	if err.Code != protocol.ErrUnauthorized {
		t.Errorf("expected UNAUTHORIZED, got %q", err.Code)
	}
}

func TestAuthorizeWithRole(t *testing.T) {
	err := AuthorizeMethod("sessions.list", "operator", true, nil)
	if err != nil {
		t.Errorf("authenticated operator should be authorized, got: %+v", err)
	}
}

func TestAuthorizeViewerCannotWrite(t *testing.T) {
	err := AuthorizeMethod("sessions.delete", "viewer", true, nil)
	if err == nil {
		t.Error("viewer should not have write scope for sessions.delete")
	}
	if err.Code != protocol.ErrForbidden {
		t.Errorf("expected FORBIDDEN, got %q", err.Code)
	}
}

func TestAuthorizeExplicitScope(t *testing.T) {
	// Viewer with explicit write scope should be allowed.
	err := AuthorizeMethod("sessions.delete", "viewer", true, []auth.Scope{auth.ScopeWrite})
	if err != nil {
		t.Errorf("explicit write scope should grant access: %+v", err)
	}
}

func TestAuthorizeAdminScopeGrantsAll(t *testing.T) {
	err := AuthorizeMethod("hooks.register", "viewer", true, []auth.Scope{auth.ScopeAdmin})
	if err != nil {
		t.Errorf("admin scope should grant access to admin method: %+v", err)
	}
}

func TestAuthorizeProcessExecRequiresApprovals(t *testing.T) {
	// Agent (read+write) lacks approvals scope.
	err := AuthorizeMethod("process.exec", "agent", true, nil)
	if err == nil {
		t.Error("agent should not have approvals scope for process.exec")
	}

	// Operator has approvals by default.
	err = AuthorizeMethod("process.exec", "operator", true, nil)
	if err != nil {
		t.Errorf("operator should have approvals scope: %+v", err)
	}
}

func TestAuthorizeUnknownMethodRequiresAdmin(t *testing.T) {
	// Unknown methods default to admin scope (fail-closed).
	err := AuthorizeMethod("some.unknown.method", "viewer", true, nil)
	if err == nil {
		t.Error("unknown method should require admin scope")
	}

	err = AuthorizeMethod("some.unknown.method", "operator", true, nil)
	if err != nil {
		t.Errorf("operator should have admin scope for unknown method: %+v", err)
	}
}

func TestAllRegisteredMethodsHaveScopes(t *testing.T) {
	// Verify every method in methodScopes is a known scope value.
	validScopes := map[auth.Scope]bool{
		auth.ScopeAdmin:     true,
		auth.ScopeRead:      true,
		auth.ScopeWrite:     true,
		auth.ScopeApprovals: true,
		auth.ScopePairing:   true,
	}
	for method, scope := range methodScopes {
		if !validScopes[scope] {
			t.Errorf("method %q has invalid scope %q", method, scope)
		}
	}
}
