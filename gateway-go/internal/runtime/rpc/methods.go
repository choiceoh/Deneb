package rpc

import (
	handlerffi "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/ffi"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
)

// RegisterBuiltinMethods registers stateless built-in RPC methods:
// protocol/security/media/parsing/markdown (FFI) and tools catalog.
//
// Domain handlers that require dependencies (sessions, telegram, health, etc.)
// are registered via method_registry.go's table-driven path.
func RegisterBuiltinMethods(d *Dispatcher) {
	d.RegisterDomain(handlerffi.ProtocolMethods())
	d.RegisterDomain(handlerffi.SecurityMethods())
	d.RegisterDomain(handlerffi.MediaMethods())
	d.RegisterDomain(handlerffi.ParsingMethods())
	d.RegisterDomain(handlerffi.MarkdownMethods())
	d.RegisterDomain(handlerskill.CatalogMethods())
}
