package rpc

import "github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"

// RegisterDomain bulk-registers all handlers from a domain handler package.
// Each domain package returns a map[string]rpcutil.HandlerFunc from its
// Methods() function; this helper iterates and registers them on the dispatcher.
// HandlerFunc is an alias for rpcutil.HandlerFunc, so no conversion is needed.
func (d *Dispatcher) RegisterDomain(methods map[string]rpcutil.HandlerFunc) {
	for name, h := range methods {
		d.Register(name, h)
	}
}
