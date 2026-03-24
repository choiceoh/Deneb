package rpc

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// publicMethods are accessible without authentication.
var publicMethods = map[string]bool{
	"health":       true,
	"health.check": true,
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
