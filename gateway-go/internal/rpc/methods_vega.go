package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// VegaDeps holds the Vega client for RPC method registration.
type VegaDeps struct {
	Client *vega.Client
}

// RegisterVegaMethods registers Vega MCP tool methods on the dispatcher.
// These forward requests to the Python Vega subprocess.
func RegisterVegaMethods(d *Dispatcher, deps VegaDeps) {
	if deps.Client == nil {
		return
	}

	// Map RPC method names to Vega tool names.
	vegaTools := map[string]string{
		"vega.ask":         "ask",
		"vega.update":      "update",
		"vega.add-action":  "add-action",
		"vega.mail-append": "mail-append",
		"vega.version":     "version",
	}

	for method, tool := range vegaTools {
		m := method
		t := tool
		d.Register(m, vegaToolHandler(deps.Client, t))
	}
}

func vegaToolHandler(client *vega.Client, tool string) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		params := req.Params
		if len(params) == 0 {
			params = json.RawMessage("{}")
		}

		result, err := client.Call(ctx, tool, params)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "vega: "+err.Error()))
		}

		// result is already json.RawMessage; wrap it in a response.
		resp, _ := protocol.NewResponseOKRaw(req.ID, result)
		return resp
	}
}
