package ffi

import (
	"context"
	"encoding/json"

	ffipkg "github.com/choiceoh/deneb/gateway-go/internal/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// VegaDeps holds the Vega backend dependency for Vega FFI RPC methods.
type VegaDeps struct {
	Backend vega.Backend
}

// VegaMethods returns handlers for Vega FFI RPC methods.
// The deps parameter provides the Vega backend for command execution.
func VegaMethods(deps VegaDeps) map[string]rpcutil.HandlerFunc {
	m := map[string]rpcutil.HandlerFunc{
		"vega.ffi.execute": vegaFFIExecute(),
		"vega.ffi.search":  vegaFFISearch(),
	}

	// Register backend-forwarding commands if a backend is available.
	if deps.Backend != nil {
		vegaCommands := map[string]string{
			"vega.ask":           "ask",
			"vega.update":        "update",
			"vega.add-action":    "add-action",
			"vega.mail-append":   "mail-append",
			"vega.version":       "version",
			"vega.memory-search": "memory-search",
			"vega.memory-update": "memory-update",
		}
		for method, cmd := range vegaCommands {
			m[method] = vegaBackendHandler(deps.Backend, cmd)
		}
	}

	return m
}

func vegaFFIExecute() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return rpcerr.New(protocol.ErrMissingParam, "params required").Response(req.ID)
		}
		result, err := ffipkg.VegaExecute(string(req.Params))
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

func vegaFFISearch() rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if len(req.Params) == 0 {
			return rpcerr.New(protocol.ErrMissingParam, "params required").Response(req.ID)
		}
		result, err := ffipkg.VegaSearch(string(req.Params))
		if err != nil {
			return rpcerr.DependencyFailed(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, json.RawMessage(result))
	}
}

// vegaBackendHandler creates an RPC handler that executes a Vega command
// via the Backend interface (Rust FFI).
func vegaBackendHandler(backend vega.Backend, cmd string) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		// Parse params as a generic map for the Backend.Execute call.
		var args map[string]any
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &args); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}

		result, err := backend.Execute(ctx, cmd, args)
		if err != nil {
			return rpcerr.Newf(protocol.ErrDependencyFailed, "vega: %s", err).Response(req.ID)
		}

		return protocol.MustResponseOKRaw(req.ID, result)
	}
}
