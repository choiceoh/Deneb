// mcp_external_tools.go — external MCP servers as deferred chat tools.
//
// Counterpart of server_http_mcp.go (which SERVES Deneb's memory to external
// AI tools): this file CONSUMES external MCP servers — Plaud's meeting
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
// Config (operator, srv4) — semicolon-separated `name[:label]=command`
// entries; the name namespaces the tools (`<name>_*`), the optional Korean
// label feeds fetch_tools' keyword search:
//
//	DENEB_MCP_SERVERS="plaud:회의·통화 녹음=npx -y @plaud-ai/mcp@latest"
//	DENEB_MCP_SERVERS="plaud=npx -y @plaud-ai/mcp;foo=uvx foo-mcp"   # empty → feature off
//
// Remote OAuth-only MCP servers work through a stdio bridge in the command
// (e.g. `npx -y mcp-remote https://…/mcp`). First-run interactive auth
// instructions arrive on the child's stderr, surfaced as "mcp server stderr"
// Info log lines.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mcpclient"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// mcpServersEnv holds the external MCP server catalog (format above).
const mcpServersEnv = "DENEB_MCP_SERVERS"

// mcpInitTimeout bounds spawn + initialize + tools/list per server. Generous
// because a cold npx run downloads the package before the server even starts.
const mcpInitTimeout = 3 * time.Minute

// mcpServerSpec is one parsed DENEB_MCP_SERVERS entry.
type mcpServerSpec struct {
	Name    string // tool namespace: tools register as <Name>_<tool>
	Label   string // optional human context folded into tool descriptions
	Cmdline string // stdio MCP server command line
}

// mcpServerNameRe bounds server names to safe tool-name-prefix material.
var mcpServerNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// parseMCPServerSpecs parses the DENEB_MCP_SERVERS value. Entries are split
// on ';', each `name[:label]=command`. The command may itself contain '='
// (only the first '=' separates). Invalid entries fail the whole parse —
// a silently dropped server is a misconfiguration the operator must see.
// Error messages never echo the command part: it may carry credentials in
// flags, and parse errors land in the operator log.
func parseMCPServerSpecs(raw string) ([]mcpServerSpec, error) {
	var specs []mcpServerSpec
	seen := make(map[string]struct{})
	for i, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, cmdline, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(cmdline) == "" {
			return nil, fmt.Errorf("entry #%d: want name[:label]=command", i+1)
		}
		name, label, _ := strings.Cut(strings.TrimSpace(key), ":")
		name = strings.ToLower(strings.TrimSpace(name))
		if !mcpServerNameRe.MatchString(name) {
			return nil, fmt.Errorf("entry #%d (name %q): server name must match %s", i+1, name, mcpServerNameRe)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("duplicate server name %q", name)
		}
		seen[name] = struct{}{}
		specs = append(specs, mcpServerSpec{
			Name:    name,
			Label:   strings.TrimSpace(label),
			Cmdline: strings.TrimSpace(cmdline),
		})
	}
	return specs, nil
}

// initExternalMCPTools discovers each configured MCP server's tools in the
// background and registers them as deferred chat tools. No-op when the env
// is unset. Boot is never blocked: a slow/failed server simply contributes
// no tools until the next gateway restart.
//
// Clients are created synchronously and published on s.externalMCP BEFORE
// any discovery goroutine runs, so later consumers (ingest tasks, health
// probes) can fetch the shared client via ExternalMCPClient at any point —
// even for a server whose chat-tool discovery failed (their calls lazily
// respawn it).
func (s *Server) initExternalMCPTools(registry *chat.ToolRegistry) {
	raw := strings.TrimSpace(os.Getenv(mcpServersEnv))
	if raw == "" {
		return
	}
	specs, err := parseMCPServerSpecs(raw)
	if err != nil {
		s.logger.Error("external mcp: invalid "+mcpServersEnv, "error", err)
		return
	}
	s.externalMCP = make(map[string]*mcpclient.Client, len(specs))
	for _, spec := range specs {
		// ShutdownCtx as process lifetime: children die with the gateway.
		client, err := mcpclient.New(s.ShutdownCtx(), strings.Fields(spec.Cmdline), s.logger)
		if err != nil {
			s.logger.Error("external mcp: invalid command", "server", spec.Name, "error", err)
			continue
		}
		s.externalMCP[spec.Name] = client
		safego.GoWithSlog(s.logger, "mcp-init-"+spec.Name, func() {
			ctx, cancel := context.WithTimeout(s.ShutdownCtx(), mcpInitTimeout)
			defer cancel()
			tools, err := client.ListTools(ctx)
			if err != nil {
				// Operator-visible: the server was explicitly configured but
				// is not usable. Most common cause on a fresh host is the
				// one-time OAuth authorization — instructions land in the
				// "mcp server stderr" log lines above this error. Close the
				// child so it doesn't linger; a later consumer call (or the
				// next restart) respawns it through the client's normal
				// backoff path.
				s.logger.Error("external mcp: tool discovery failed (check 'mcp server stderr' lines for auth instructions)",
					"server", spec.Name, "error", err)
				client.CloseProcess()
				return
			}
			if len(tools) == 0 {
				s.logger.Warn("external mcp: server reported no tools", "server", spec.Name)
				client.CloseProcess()
				return
			}
			names := registerMCPServerTools(registry, spec, tools, client.CallTool)
			s.logger.Info("external mcp tools registered (deferred, activate via fetch_tools)",
				"server", spec.Name, "count", len(names), "tools", strings.Join(names, ","))
		})
	}
}

// registerMCPServerTools registers one server's discovered tools as deferred
// chat tools under the spec's namespace, returning the registered names.
// Pure with respect to process/IO so tests can drive it with fake tools.
func registerMCPServerTools(
	registry *chat.ToolRegistry,
	spec mcpServerSpec,
	tools []mcpclient.ToolInfo,
	call func(ctx context.Context, name string, args json.RawMessage) (string, error),
) []string {
	desc := func(toolDesc string) string {
		if spec.Label != "" {
			return fmt.Sprintf("%s MCP(%s): %s", spec.Label, spec.Name, toolDesc)
		}
		return fmt.Sprintf("MCP(%s): %s", spec.Name, toolDesc)
	}
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		name := clampToolName(spec.Name+"_"+sanitizeMCPToolName(t.Name), t.Name)
		names = append(names, name)
		remote := t.Name
		registry.RegisterTool(chat.ToolDef{
			Name:        name,
			Description: desc(t.Description),
			InputSchema: t.InputSchema,
			Deferred:    true,
			Fn: func(ctx context.Context, input json.RawMessage) (string, error) {
				return call(ctx, remote, input)
			},
		})
	}
	return names
}

// maxLLMToolNameLen is the strictest tool-name length across the providers
// the gateway talks to (OpenAI-compatible caps at 64; Anthropic allows 128).
const maxLLMToolNameLen = 64

// clampToolName bounds a namespaced tool name to maxLLMToolNameLen. The tail
// is replaced with a hash of the ORIGINAL remote name so two long remote
// names that share a prefix cannot collide after truncation. 6 hash bytes
// (48 bits): RegisterTool silently replaces on name collision, so the suffix
// must be wide enough that a hostile server cannot feasibly brute-force a
// clash with another tool's clamped name.
func clampToolName(name, remote string) string {
	if len(name) <= maxLLMToolNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(remote))
	suffix := "_" + hex.EncodeToString(sum[:6])
	return name[:maxLLMToolNameLen-len(suffix)] + suffix
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
