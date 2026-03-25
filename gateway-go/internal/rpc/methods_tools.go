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
}

// RegisterToolMethods registers tool-related RPC methods.
func RegisterToolMethods(d *Dispatcher, deps ToolDeps) {
	d.Register("tools.invoke", toolsInvoke(deps))
	d.Register("tools.list", toolsList(deps))
	d.Register("tools.status", toolsStatus(deps))
}

// toolsInvoke handles "tools.invoke" — executes a tool by name.
// Native execution via the process manager is supported for bash/exec tools.
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

		// Non-bash/exec tools are not available in standalone Go gateway.
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrUnavailable, "tool "+p.Tool+" not available in standalone mode"))
	}
}

// toolsExecLocal executes a bash/exec tool locally using the process manager.
func toolsExecLocal(ctx context.Context, req *protocol.RequestFrame, deps ToolDeps, tool string, args map[string]any, dryRun bool) *protocol.ResponseFrame {
	if dryRun {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
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

	resp := protocol.MustResponseOK(req.ID, result)
	return resp
}

// toolsList handles "tools.list" — returns the available tool catalog.
// Enumerates core tools from the static catalog (same source as tools.catalog).
func toolsList(_ ToolDeps) HandlerFunc {
	// Pre-compute the flat tool list at registration time.
	groups := buildCoreToolCatalog()
	tools := make([]map[string]any, 0, 24)
	for _, g := range groups {
		for _, t := range g.Tools {
			tools = append(tools, map[string]any{
				"id":          t.ID,
				"label":       t.Label,
				"description": t.Description,
				"source":      t.Source,
				"group":       g.ID,
			})
		}
	}

	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"tools": tools,
		})
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

		resp := protocol.MustResponseOK(req.ID, tracked)
		return resp
	}
}
