package rpc

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// AuthorizeMethod checks whether the given role allows calling the method.
// Public methods (health, health.check) are open to all.
// All other methods require authentication and the operator role.
func AuthorizeMethod(method string, role string, authenticated bool) *protocol.ErrorShape {
	if publicMethods[method] {
		return nil
	}
	if !authenticated {
		return protocol.NewError(protocol.ErrUnauthorized, fmt.Sprintf("authentication required for method: %q", method))
	}
	if role != "operator" && role != "agent" {
		return protocol.NewError(protocol.ErrForbidden, fmt.Sprintf("insufficient permissions for %q (role=%s)", method, role))
	}
	return nil
}
