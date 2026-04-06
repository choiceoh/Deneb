package rpc

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RequiredScope returns the scope needed to call the given method.
// Returns ScopeAdmin for unknown methods (fail-closed). When adding new RPC
// methods, register them in method_scopes.yaml to avoid unintentional admin-only access.
func RequiredScope(method string) auth.Scope {
	if scope, ok := methodScopes[method]; ok {
		return scope
	}
	return auth.ScopeAdmin
}

// AuthorizeMethod checks whether the given role and scopes allow calling the method.
// Returns nil if authorized, or an ErrorShape if not.
func AuthorizeMethod(method string, role string, authenticated bool, scopes []auth.Scope) *protocol.ErrorShape {
	if publicMethods[method] {
		return nil
	}
	if !authenticated {
		return protocol.NewError(protocol.ErrUnauthorized, fmt.Sprintf("authentication required for method: %q", method))
	}
	if role == "" {
		return protocol.NewError(protocol.ErrUnauthorized, "no role assigned")
	}

	required := RequiredScope(method)
	if err := auth.CheckPermission(auth.Role(role), scopes, required); err != nil {
		return protocol.NewError(protocol.ErrForbidden, fmt.Sprintf("insufficient permissions for %q: %v", method, err))
	}

	return nil
}
