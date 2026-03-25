package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// VegaDeps holds the Vega backend for RPC method registration.
type VegaDeps struct {
	Backend vega.Backend
}

// RegisterVegaMethods registers Vega RPC methods on the dispatcher.
// These forward requests to the Rust FFI Vega backend.
func RegisterVegaMethods(d *Dispatcher, deps VegaDeps) {
	if deps.Backend == nil {
		return
	}

	// Map RPC method names to Vega command names.
	vegaCommands := map[string]string{
		"vega.ask":         "ask",
		"vega.update":      "update",
		"vega.add-action":  "add-action",
		"vega.mail-append": "mail-append",
		"vega.version":     "version",
	}

	for method, cmd := range vegaCommands {
		m := method
		c := cmd
		d.Register(m, vegaBackendHandler(deps.Backend, c))
	}
}

// ---------------------------------------------------------------------------
// Vega FFI RPC methods (Rust FFI)
// ---------------------------------------------------------------------------

func vegaFFIExecute() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}
		result, err := ffi.VegaExecute(string(req.Params))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

func vegaFFISearch() HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "params required"))
		}
		result, err := ffi.VegaSearch(string(req.Params))
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, err.Error()))
		}
		resp := protocol.MustResponseOK(req.ID, json.RawMessage(result))
		return resp
	}
}

// vegaBackendHandler creates an RPC handler that executes a Vega command
// via the Backend interface (Rust FFI).
func vegaBackendHandler(backend vega.Backend, cmd string) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// Parse params as a generic map for the Backend.Execute call.
		var args map[string]any
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &args); err != nil {
				return protocol.NewResponseError(req.ID, protocol.NewError(
					protocol.ErrMissingParam, "invalid params: "+err.Error()))
			}
		}

		result, err := backend.Execute(ctx, cmd, args)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "vega: "+err.Error()))
		}

		resp := protocol.MustResponseOKRaw(req.ID, result)
		return resp
	}
}
