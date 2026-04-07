// Tools RPC handlers (tools.*).
package skill

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// ToolDeps — tools.* handlers
// ---------------------------------------------------------------------------

// ToolDeps holds dependencies for tool invocation RPC methods.
type ToolDeps struct {
	Processes *process.Manager
}

// ToolMethods returns all tools.* RPC handler methods (invoke, list, status).
func ToolMethods(deps ToolDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"tools.invoke": toolsInvoke(deps),
		"tools.list":   toolsList(deps),
		"tools.status": toolsStatus(deps),
	}
}

// toolsInvoke handles "tools.invoke" — executes a tool by name.
// Native execution via the process manager is supported for bash/exec tools.
// Uses the manual unmarshal pattern because toolsExecLocal needs both ctx and req.
func toolsInvoke(deps ToolDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Tool       string         `json:"tool"`
			Action     string         `json:"action,omitempty"`
			Args       map[string]any `json:"args,omitempty"`
			SessionKey string         `json:"sessionKey,omitempty"`
			DryRun     bool           `json:"dryRun,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Tool == "" {
			return rpcerr.MissingParam("tool").Response(req.ID)
		}

		// For bash/exec tools, execute locally via process manager.
		if (p.Tool == "bash" || p.Tool == "exec") && deps.Processes != nil {
			return toolsExecLocal(ctx, req, deps, p.Tool, p.Args, p.DryRun)
		}

		// Non-bash/exec tools are not available in standalone Go gateway.
		return rpcerr.Unavailable("tool " + p.Tool + " not available in standalone mode").Response(req.ID)
	}
}

// toolsExecLocal executes a bash/exec tool locally using the process manager.
func toolsExecLocal(ctx context.Context, req *protocol.RequestFrame, deps ToolDeps, tool string, args map[string]any, dryRun bool) *protocol.ResponseFrame {
	if dryRun {
		return rpcutil.RespondOK(req.ID, map[string]any{
			"tool":   tool,
			"dryRun": true,
			"args":   args,
		})
	}

	command, _ := args["command"].(string)
	if command == "" {
		return rpcerr.MissingParam("command for " + tool + " tool").Response(req.ID)
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

	return rpcutil.RespondOK(req.ID, result)
}

// toolsList handles "tools.list" — returns the available tool catalog.
// Enumerates core tools from the static catalog (same source as tools.catalog).
func toolsList(_ ToolDeps) rpcutil.HandlerFunc {
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
		return rpcutil.RespondOK(req.ID, map[string]any{
			"tools": tools,
		})
	}
}

// toolsStatus handles "tools.status" — returns status of a running tool execution.
func toolsStatus(deps ToolDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if deps.Processes == nil {
			return nil, rpcerr.NotFound("process tracking")
		}
		tracked := deps.Processes.Get(p.ID)
		if tracked == nil {
			return nil, rpcerr.NotFound("tool execution")
		}
		return tracked, nil
	})
}
