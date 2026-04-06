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
					requiredProp("sessionKey", "string", "The session key to send to"),
					requiredProp("message", "string", "The message to send to the agent"),
				),
			},
		},
		{
			rpcMethod: "chat.btw",
			tool: Tool{
				Name:        "deneb_chat_btw",
				Description: "Ask Deneb a side question without affecting the main conversation context. Useful for quick lookups.",
				InputSchema: objectSchema(
					requiredProp("question", "string", "The side question to ask"),
					requiredProp("sessionKey", "string", "The active session key"),
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
					requiredProp("key", "string", "The session key to send to"),
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
					requiredProp("keys", "array", "Array of session keys to preview"),
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
				InputSchema: objectSchema(),
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

		// --- Inter-agent bridge ---
		{
			rpcMethod: "bridge.send",
			tool: Tool{
				Name:        "deneb_bridge_send",
				Description: "Send a message to the Deneb main agent via the inter-agent bridge. Injects into the active Telegram session and triggers an LLM run so the agent responds.",
				InputSchema: objectSchema(
					requiredProp("message", "string", "The message to send"),
					prop("source", "string", "Sender identity (default: 'claude-code')"),
				),
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
					prop("args", "object", "Tool-specific input parameters as JSON object"),
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
					prop("timeoutMs", "integer", "Timeout in milliseconds (default: 30000)"),
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

		// --- Autoresearch ---
		{
			rpcMethod: "autoresearch.status",
			tool: Tool{
				Name:        "deneb_autoresearch_status",
				Description: "Get the current status of the autoresearch experiment runner including iteration counts, best metric, and latest result.",
				InputSchema: objectSchema(),
			},
		},
		{
			rpcMethod: "autoresearch.start",
			tool: Tool{
				Name:        "deneb_autoresearch_start",
				Description: "Start the autoresearch experiment loop. Config must exist at workdir/.autoresearch/config.json (use deneb_autoresearch_config first).",
				InputSchema: objectSchema(
					requiredProp("workdir", "string", "Workspace directory containing .autoresearch/config.json"),
				),
			},
		},
		{
			rpcMethod: "autoresearch.stop",
			tool: Tool{
				Name:        "deneb_autoresearch_stop",
				Description: "Stop the running autoresearch experiment and return final summary.",
				InputSchema: objectSchema(),
			},
		},
		{
			rpcMethod: "autoresearch.results",
			tool: Tool{
				Name:        "deneb_autoresearch_results",
				Description: "Get autoresearch experiment results as structured JSON rows with iteration, metric, hypothesis, and kept status.",
				InputSchema: objectSchema(
					prop("workdir", "string", "Workspace directory (defaults to current experiment workdir)"),
					prop("format", "string", "Output format: 'json' (default, structured rows + trend) or 'tsv' (raw tab-separated)"),
				),
			},
		},
		{
			rpcMethod: "autoresearch.config",
			tool: Tool{
				Name:        "deneb_autoresearch_config",
				Description: "Initialize or update autoresearch experiment configuration. Creates .autoresearch/config.json without starting the experiment.",
				InputSchema: objectSchema(
					requiredProp("workdir", "string", "Workspace directory for the experiment"),
					requiredProp("target_files", "array", "Files the agent may modify (relative to workdir)"),
					requiredProp("metric_cmd", "string", "Shell command that runs the experiment and outputs the metric"),
					requiredProp("metric_name", "string", "Human-readable metric name"),
					requiredProp("metric_direction", "string", "'minimize' or 'maximize'"),
					requiredProp("branch_tag", "string", "Suffix for experiment branch (autoresearch/<tag>)"),
					prop("time_budget_sec", "integer", "Time budget per experiment in seconds (default: 300)"),
					prop("max_iterations", "integer", "Max iterations before auto-stop (default: 30)"),
					prop("model", "string", "LLM model for hypothesis generation"),
					prop("metric_pattern", "string", "Regex with capture group to extract metric from output"),
					prop("cache_enabled", "boolean", "Enable persistent cache across iterations"),
				),
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
	tools  []toolDef
	byName map[string]*toolDef
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
