package mcp

// allTools returns the curated set of MCP tools mapped to gateway RPC methods.
func allTools() []toolDef {
	return []toolDef{
		// --- Chat & Interaction ---
		{
			rpcMethod: "chat.send",
			tool: Tool{
				Name:        "deneb_chat_send",
				Description: "Send a message to Deneb's AI agent and get a response. The agent can execute tools, write code, and perform complex tasks.",
				InputSchema: objectSchema(
					prop("message", "string", "The message to send to the agent"),
					prop("session_key", "string", "Optional session key. If omitted, uses the active session"),
				),
			},
		},
		{
			rpcMethod: "chat.btw",
			tool: Tool{
				Name:        "deneb_chat_btw",
				Description: "Ask Deneb a side question without affecting the main conversation context. Useful for quick lookups.",
				InputSchema: objectSchema(
					prop("message", "string", "The side question to ask"),
				),
			},
		},

		// --- Session Management ---
		{
			rpcMethod: "sessions.list",
			tool: Tool{
				Name:        "deneb_sessions_list",
				Description: "List all active sessions with their status and metadata.",
				InputSchema: objectSchema(),
			},
		},
		{
			rpcMethod: "sessions.create",
			tool: Tool{
				Name:        "deneb_sessions_create",
				Description: "Create a new session for independent conversation/task execution.",
				InputSchema: objectSchema(
					prop("kind", "string", "Session kind: 'chat' or 'agent'"),
				),
			},
		},
		{
			rpcMethod: "sessions.send",
			tool: Tool{
				Name:        "deneb_sessions_send",
				Description: "Send a message to a specific session by key.",
				InputSchema: objectSchema(
					requiredProp("session_key", "string", "The session key to send to"),
					requiredProp("message", "string", "The message to send"),
				),
			},
		},
		{
			rpcMethod: "sessions.preview",
			tool: Tool{
				Name:        "deneb_sessions_history",
				Description: "Get a summary/preview of a session's conversation history.",
				InputSchema: objectSchema(
					requiredProp("session_key", "string", "The session key"),
				),
			},
		},

		// --- Memory ---
		{
			rpcMethod: "memory.search",
			tool: Tool{
				Name:        "deneb_memory_search",
				Description: "Search Deneb's persistent memory using hybrid search (vector + keyword). Returns relevant memories and diary entries.",
				InputSchema: objectSchema(
					requiredProp("query", "string", "Search query"),
					prop("limit", "integer", "Maximum number of results (default: 10)"),
				),
			},
		},
		{
			rpcMethod: "memory.set",
			tool: Tool{
				Name:        "deneb_memory_set",
				Description: "Store a new entry in Deneb's persistent memory.",
				InputSchema: objectSchema(
					requiredProp("key", "string", "Memory key/identifier"),
					requiredProp("value", "string", "Content to store"),
				),
			},
		},

		// --- Vega Search ---
		{
			rpcMethod: "vega.ffi.search",
			tool: Tool{
				Name:        "deneb_vega_search",
				Description: "Perform semantic search across Deneb's knowledge base using Vega (FTS5 + optional vector search).",
				InputSchema: objectSchema(
					requiredProp("query", "string", "Search query"),
					prop("limit", "integer", "Maximum results (default: 10)"),
				),
			},
		},
		{
			rpcMethod: "vega.ask",
			tool: Tool{
				Name:        "deneb_vega_ask",
				Description: "Ask Vega a question with retrieval-augmented generation. Searches knowledge base and synthesizes an answer.",
				InputSchema: objectSchema(
					requiredProp("query", "string", "The question to ask"),
				),
			},
		},

		// --- System ---
		{
			rpcMethod: "gateway.identity.get",
			tool: Tool{
				Name:        "deneb_system_status",
				Description: "Get Deneb gateway system status including version, hostname, architecture, OS, and uptime.",
				InputSchema: objectSchema(),
			},
		},
		{
			rpcMethod: "monitoring.activity",
			tool: Tool{
				Name:        "deneb_monitoring",
				Description: "Get current system activity metrics: active sessions, running processes, memory usage.",
				InputSchema: objectSchema(),
			},
		},
		{
			rpcMethod: "providers.list",
			tool: Tool{
				Name:        "deneb_providers_list",
				Description: "List configured LLM providers and their status.",
				InputSchema: objectSchema(),
			},
		},
		{
			rpcMethod: "models.list",
			tool: Tool{
				Name:        "deneb_models_list",
				Description: "List all available LLM models across providers.",
				InputSchema: objectSchema(),
			},
		},
		{
			rpcMethod: "config.get",
			tool: Tool{
				Name:        "deneb_config_get",
				Description: "Read the current gateway configuration.",
				InputSchema: objectSchema(
					prop("path", "string", "Optional dot-separated config path to read a specific section"),
				),
			},
		},
		{
			rpcMethod: "skills.status",
			tool: Tool{
				Name:        "deneb_skills_status",
				Description: "Get the status of all installed skills and their available commands.",
				InputSchema: objectSchema(),
			},
		},

		// --- Meta / Escape hatch ---
		{
			rpcMethod: "tools.invoke",
			tool: Tool{
				Name:        "deneb_tools_invoke",
				Description: "Invoke any Deneb agent tool by name (exec, read, write, edit, grep, git, test, memory, web, http, etc.). Use this for tools not directly exposed as MCP tools.",
				InputSchema: objectSchema(
					requiredProp("tool", "string", "Tool name (e.g. 'exec', 'read', 'grep', 'git')"),
					prop("input", "object", "Tool-specific input parameters as JSON object"),
				),
			},
		},
		{
			rpcMethod: "process.exec",
			tool: Tool{
				Name:        "deneb_process_exec",
				Description: "Execute a system command on the DGX Spark server.",
				InputSchema: objectSchema(
					requiredProp("command", "string", "The command to execute"),
					prop("timeout", "integer", "Timeout in seconds (default: 30)"),
					prop("background", "boolean", "Run in background (default: false)"),
				),
			},
		},
		{
			rpcMethod: "cron.list",
			tool: Tool{
				Name:        "deneb_cron_list",
				Description: "List all scheduled cron jobs and their status.",
				InputSchema: objectSchema(),
			},
		},
	}
}

// toolDef pairs an MCP tool with its gateway RPC method.
type toolDef struct {
	rpcMethod string
	tool      Tool
}

// ToolRegistry holds the tool definitions indexed by MCP tool name.
type ToolRegistry struct {
	tools    []toolDef
	byName   map[string]*toolDef
}

// NewToolRegistry creates and indexes the tool registry.
func NewToolRegistry() *ToolRegistry {
	tools := allTools()
	r := &ToolRegistry{
		tools:  tools,
		byName: make(map[string]*toolDef, len(tools)),
	}
	for i := range r.tools {
		r.byName[r.tools[i].tool.Name] = &r.tools[i]
	}
	return r
}

// List returns all MCP tool definitions.
func (r *ToolRegistry) List() []Tool {
	out := make([]Tool, len(r.tools))
	for i, td := range r.tools {
		out[i] = td.tool
	}
	return out
}

// Lookup returns the RPC method for the given MCP tool name.
func (r *ToolRegistry) Lookup(name string) (rpcMethod string, ok bool) {
	td, ok := r.byName[name]
	if !ok {
		return "", false
	}
	return td.rpcMethod, true
}

// --- Schema helpers ---

type schemaProp struct {
	name        string
	typ         string
	description string
	required    bool
}

func prop(name, typ, description string) schemaProp {
	return schemaProp{name: name, typ: typ, description: description}
}

func requiredProp(name, typ, description string) schemaProp {
	return schemaProp{name: name, typ: typ, description: description, required: true}
}

func objectSchema(props ...schemaProp) map[string]any {
	schema := map[string]any{
		"type": "object",
	}
	if len(props) == 0 {
		return schema
	}
	properties := make(map[string]any, len(props))
	var required []string
	for _, p := range props {
		properties[p.name] = map[string]any{
			"type":        p.typ,
			"description": p.description,
		}
		if p.required {
			required = append(required, p.name)
		}
	}
	schema["properties"] = properties
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
