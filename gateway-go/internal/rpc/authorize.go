package rpc

import "github.com/choiceoh/deneb/gateway-go/pkg/protocol"

// publicMethods are accessible without authentication.
var publicMethods = map[string]bool{
	"health": true,
}

// AuthorizeMethod checks whether the given role is allowed to call the method.
// Returns nil if authorized, or an ErrorShape if not.
func AuthorizeMethod(method string, role string, authenticated bool) *protocol.ErrorShape {
	if publicMethods[method] {
		return nil
	}
	if !authenticated {
		return protocol.NewError(protocol.ErrUnauthorized, "authentication required for method: "+method)
	}
	if role == "" {
		return protocol.NewError(protocol.ErrUnauthorized, "no role assigned")
	}
	return nil
}
