package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ToolDeps holds dependencies for tool invocation RPC methods.
type ToolDeps struct {
	Deps
	Processes *process.Manager
	Forwarder Forwarder
}

// RegisterToolMethods registers tool-related RPC methods.
func RegisterToolMethods(d *Dispatcher, deps ToolDeps) {
	d.Register("tools.invoke", toolsInvoke(deps))
	d.Register("tools.list", toolsList(deps))
	d.Register("tools.status", toolsStatus(deps))
}

// toolsInvoke handles "tools.invoke" — executes a tool by name.
// For now, this validates params and forwards to the bridge. Native execution
// using the process manager is supported for bash/exec tools.
func toolsInvoke(deps ToolDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Tool       string         `json:"tool"`
			Action     string         `json:"action,omitempty"`
			Args       map[string]any `json:"args,omitempty"`
			SessionKey string         `json:"sessionKey,omitempty"`
			DryRun     bool           `json:"dryRun,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid tool params: "+err.Error()))
		}
		if p.Tool == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "tool is required"))
		}

		// For bash/exec tools, execute locally via process manager.
		if (p.Tool == "bash" || p.Tool == "exec") && deps.Processes != nil {
			return toolsExecLocal(ctx, req, deps, p.Tool, p.Args, p.DryRun)
		}

		// Forward all other tools to the Node.js bridge.
		if deps.Forwarder == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "no bridge available for tool execution"))
		}

		forwardReq := &protocol.RequestFrame{
			Type:   protocol.FrameTypeRequest,
			ID:     req.ID,
			Method: "tools.invoke",
			Params: req.Params,
		}
		resp, err := deps.Forwarder.Forward(ctx, forwardReq)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "tool bridge error: "+err.Error()))
		}
		return resp
	}
}

// toolsExecLocal executes a bash/exec tool locally using the process manager.
func toolsExecLocal(ctx context.Context, req *protocol.RequestFrame, deps ToolDeps, tool string, args map[string]any, dryRun bool) *protocol.ResponseFrame {
	if dryRun {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"tool":   tool,
			"dryRun": true,
			"args":   args,
		})
		return resp
	}

	command, _ := args["command"].(string)
	if command == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "command is required for "+tool+" tool"))
	}

	var execArgs []string
	if tool == "bash" {
		execArgs = []string{"-c", command}
		command = "bash"
	}

	timeoutMs := int64(30000)
	if t, ok := args["timeoutMs"].(float64); ok && t > 0 {
		timeoutMs = int64(t)
		// Cap at 5 minutes to prevent unbounded execution.
		const maxTimeoutMs = int64(5 * 60 * 1000)
		if timeoutMs > maxTimeoutMs {
			timeoutMs = maxTimeoutMs
		}
	}

	workDir, _ := args["workingDir"].(string)

	result := deps.Processes.Execute(ctx, process.ExecRequest{
		Command:    command,
		Args:       execArgs,
		WorkingDir: workDir,
		TimeoutMs:  timeoutMs,
	})

	resp, _ := protocol.NewResponseOK(req.ID, result)
	return resp
}

// toolsList handles "tools.list" — returns the available tool catalog.
// Forwards to bridge for the full tool list.
func toolsList(deps ToolDeps) HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.Forwarder == nil {
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"tools": []any{},
			})
			return resp
		}

		forwardReq := &protocol.RequestFrame{
			Type:   protocol.FrameTypeRequest,
			ID:     req.ID,
			Method: "tools.list",
			Params: req.Params,
		}
		resp, err := deps.Forwarder.Forward(ctx, forwardReq)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "tool list bridge error: "+err.Error()))
		}
		return resp
	}
}

// toolsStatus handles "tools.status" — returns status of a running tool execution.
func toolsStatus(deps ToolDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.ID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "id is required"))
		}

		if deps.Processes == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "process tracking not available"))
		}

		tracked := deps.Processes.Get(p.ID)
		if tracked == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "tool execution not found"))
		}

		resp, _ := protocol.NewResponseOK(req.ID, tracked)
		return resp
	}
}
