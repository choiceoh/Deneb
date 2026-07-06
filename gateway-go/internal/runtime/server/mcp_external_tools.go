// mcp_external_tools.go — external MCP servers as deferred chat tools.
//
// Counterpart of server_http_mcp.go (which SERVES Deneb's memory to external
// AI tools): this file CONSUMES an external MCP server — Plaud's meeting
// recorder first — as chat tools. Each discovered tool is registered
// Deferred, so it never enters the initial LLM tool array: the model
// activates it via fetch_tools on demand, and the deferred name+description
// listing folds into the static prompt-cache key like any other deferred
// tool. Discovery is async (seconds after boot): a session whose first turn
// races that window sees the deferred listing appear on its next turn — one
// static-cache miss, self-healing. Steady-state sessions are byte-stable;
// the toolset never changes again until restart (prompt-cache.md Rule B).
//
// Why this exists: meeting_harvest.go captures post-meeting outcomes by
// ASKING the operator. With Plaud tools the agent can pull the actual
// transcript/summary instead and file it into the project 로그.md via the
// standing wiki instruction — same flywheel, no manual capture.
//
// Config (operator, srv4):
//
//	DENEB_PLAUD_MCP_CMD="npx -y @plaud-ai/mcp@latest"   # empty → feature off
//
// First run requires a one-time interactive OAuth authorization; the npx
// wrapper prints instructions on stderr, which the client surfaces as
// "mcp server stderr" Info log lines.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mcpclient"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// plaudMCPCmdEnv holds the command line for Plaud's stdio MCP server.
const plaudMCPCmdEnv = "DENEB_PLAUD_MCP_CMD"

// plaudToolPrefix namespaces external tool names away from core tools
// (tool-interception-gap.md §7: never rely on last-writer-wins).
const plaudToolPrefix = "plaud_"

// mcpInitTimeout bounds spawn + initialize + tools/list. Generous because a
// cold npx run downloads the package before the server even starts.
const mcpInitTimeout = 3 * time.Minute

// initExternalMCPTools discovers the configured Plaud MCP server's tools in
// the background and registers them as deferred chat tools. No-op when the
// env is unset. Boot is never blocked: on a slow/failed init the toolset
// simply lacks the plaud_* tools until the next gateway restart.
func (s *Server) initExternalMCPTools(registry *chat.ToolRegistry) {
	cmdline := strings.TrimSpace(os.Getenv(plaudMCPCmdEnv))
	if cmdline == "" {
		return
	}
	// ShutdownCtx as process lifetime: the child dies with the gateway.
	client, err := mcpclient.New(s.ShutdownCtx(), strings.Fields(cmdline), s.logger)
	if err != nil {
		s.logger.Error("plaud mcp: invalid command", "error", err)
		return
	}

	safego.GoWithSlog(s.logger, "plaud-mcp-init", func() {
		ctx, cancel := context.WithTimeout(s.ShutdownCtx(), mcpInitTimeout)
		defer cancel()
		tools, err := client.ListTools(ctx)
		if err != nil {
			// Operator-visible: the feature was explicitly configured but is
			// not usable. Most common cause on a fresh host is the one-time
			// OAuth authorization — instructions land in the "mcp server
			// stderr" log lines above this error.
			s.logger.Error("plaud mcp: tool discovery failed (check 'mcp server stderr' lines for auth instructions)",
				"cmd", cmdline, "error", err)
			return
		}
		if len(tools) == 0 {
			s.logger.Warn("plaud mcp: server reported no tools", "cmd", cmdline)
			return
		}
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			name := plaudToolPrefix + sanitizeMCPToolName(t.Name)
			names = append(names, name)
			remote := t.Name
			registry.RegisterTool(chat.ToolDef{
				Name:        name,
				Description: "Plaud(회의·통화 녹음) MCP: " + t.Description,
				InputSchema: t.InputSchema,
				Deferred:    true,
				Fn: func(ctx context.Context, input json.RawMessage) (string, error) {
					return client.CallTool(ctx, remote, input)
				},
			})
		}
		s.logger.Info("plaud mcp tools registered (deferred, activate via fetch_tools)",
			"count", len(tools), "tools", strings.Join(names, ","))
	})
}

// sanitizeMCPToolName maps an arbitrary MCP tool name onto the character set
// LLM APIs accept for tool names ([a-zA-Z0-9_-], bounded length).
func sanitizeMCPToolName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	out := sb.String()
	if out == "" {
		out = fmt.Sprintf("tool_%x", len(name))
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
