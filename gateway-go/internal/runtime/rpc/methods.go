package rpc

import (
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
)

// RegisterBuiltinMethods registers stateless built-in RPC methods:
// tools catalog.
//
// Domain handlers that require dependencies (sessions, telegram, health, etc.)
// are registered via method_registry.go's table-driven path.
func RegisterBuiltinMethods(d *Dispatcher) {
	d.RegisterDomain(handlerskill.CatalogMethods())
}
