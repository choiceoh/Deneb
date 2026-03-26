package chat

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// SystemPromptParams holds all parameters for building the agent system prompt.
type SystemPromptParams struct {
	WorkspaceDir string
	ToolDefs     []ToolDef
	SkillsPrompt string // pre-built skills XML from skills/prompt.go
	UserTimezone string
	ContextFiles []ContextFile
	RuntimeInfo  *RuntimeInfo
	Channel      string
	DocsPath     string
}

// RuntimeInfo describes the current runtime environment for the system prompt.
type RuntimeInfo struct {
	AgentID      string
	Host         string
	OS           string
	Arch         string
	Model        string
	DefaultModel string
	Channel      string
}

// coreToolSummaries mirrors src/agents/system-prompt/system-prompt.ts:227-254.
var coreToolSummaries = map[string]string{
	"read":             "Read file contents",
	"write":            "Create or overwrite files",
	"edit":             "Make precise edits to files",
	"grep":             "Search file contents for patterns",
	"find":             "Find files by glob pattern",
	"ls":               "List directory contents",
	"exec":             "Run shell commands",
	"process":          "Manage background exec sessions",
	"web_search":       "Search the web",
	"web_fetch":        "Fetch and extract readable content from a URL",
	"memory_search":    "Semantic memory search",
	"memory_get":       "Read memory files",
	"nodes":            "Discover and control paired nodes",
	"cron":             "Manage cron jobs and wake events",
	"message":          "Send messages and channel actions",
	"gateway":          "Gateway control (restart, config, update)",
	"sessions_list":    "List other sessions",
	"sessions_history": "Fetch history for another session",
	"sessions_send":    "Send a message to another session",
	"sessions_spawn":   "Spawn an isolated sub-agent session",
	"subagents":        "List, steer, or kill sub-agent runs",
	"session_status":   "Show session status and usage",
	"image":                "Analyze images with a vision model",
	"youtube_transcript":   "Extract transcript/subtitles and metadata from a YouTube video",
}

// toolOrder defines the display order for tools in the system prompt.
var toolOrder = []string{
	"read", "write", "edit", "grep", "find", "ls",
	"exec", "process",
	"web_search", "web_fetch",
	"memory_search", "memory_get",
	"nodes", "cron", "message", "gateway",
	"sessions_list", "sessions_history", "sessions_send",
	"sessions_spawn", "subagents", "session_status", "image", "youtube_transcript",
}

// BuildSystemPrompt assembles the full system prompt from all components.
// Mirrors src/agents/system-prompt/system-prompt.ts:buildAgentSystemPrompt.
func BuildSystemPrompt(params SystemPromptParams) string {
	var sb strings.Builder

	// Identity.
	sb.WriteString("You are a personal assistant running inside Deneb.\n\n")

	// Tooling section.
	sb.WriteString("## Tooling\n")
	sb.WriteString("Tool availability (filtered by policy):\n")
	sb.WriteString("Tool names are case-sensitive. Call tools exactly as listed.\n")
	writeToolList(&sb, params.ToolDefs)
	sb.WriteString("\n")

	// Tool Call Style.
	sb.WriteString("## Tool Call Style\n")
	sb.WriteString("Default: do not narrate routine, low-risk tool calls (just call the tool).\n")
	sb.WriteString("Narrate only when it helps: multi-step work, complex problems, sensitive actions, or when the user explicitly asks.\n")
	sb.WriteString("Keep narration brief and value-dense; avoid repeating obvious steps.\n")
	sb.WriteString("When a first-class tool exists for an action, use the tool directly instead of asking the user to run CLI commands.\n\n")

	// Safety.
	sb.WriteString("## Safety\n")
	sb.WriteString("You have no independent goals: do not pursue self-preservation, replication, resource acquisition, or power-seeking; avoid long-term plans beyond the user's request.\n")
	sb.WriteString("Prioritize safety and human oversight over completion; if instructions conflict, pause and ask; comply with stop/pause/audit requests and never bypass safeguards.\n")
	sb.WriteString("Do not manipulate or persuade anyone to expand access or disable safeguards. Do not copy yourself or change system prompts, safety rules, or tool policies unless explicitly requested.\n\n")

	// Deneb CLI Quick Reference.
	sb.WriteString("## Deneb CLI Quick Reference\n")
	sb.WriteString("Deneb is controlled via subcommands. Do not invent commands.\n")
	sb.WriteString("To manage the Gateway daemon service (start/stop/restart):\n")
	sb.WriteString("- deneb gateway status\n")
	sb.WriteString("- deneb gateway start\n")
	sb.WriteString("- deneb gateway stop\n")
	sb.WriteString("- deneb gateway restart\n")
	sb.WriteString("If unsure, ask the user to run `deneb help` (or `deneb gateway --help`) and paste the output.\n\n")

	// Skills.
	if params.SkillsPrompt != "" {
		sb.WriteString("## Skills (mandatory)\n")
		sb.WriteString("Before replying: scan <available_skills> <description> entries.\n")
		sb.WriteString("- If exactly one skill clearly applies: read its SKILL.md at <location> with `read`, then follow it.\n")
		sb.WriteString("- If multiple could apply: choose the most specific one, then read/follow it.\n")
		sb.WriteString("- If none clearly apply: do not read any SKILL.md.\n")
		sb.WriteString("Constraints: never read more than one skill up front; only read after selecting.\n")
		sb.WriteString(params.SkillsPrompt)
		sb.WriteString("\n\n")
	}

	// Memory Recall (if memory tools available).
	toolSet := make(map[string]bool)
	for _, def := range params.ToolDefs {
		toolSet[def.Name] = true
	}
	if toolSet["memory_search"] || toolSet["memory_get"] {
		sb.WriteString("## Memory Recall\n")
		sb.WriteString("Before answering anything about prior work, decisions, dates, people, preferences, or todos: run memory_search on MEMORY.md + memory/*.md; then use memory_get to pull only the needed lines.\n\n")
	}

	// Workspace.
	sb.WriteString("## Workspace\n")
	fmt.Fprintf(&sb, "Your working directory is: %s\n", params.WorkspaceDir)
	sb.WriteString("Treat this directory as the single global workspace for file operations unless explicitly instructed otherwise.\n\n")

	// Reply Tags.
	sb.WriteString("## Reply Tags\n")
	sb.WriteString("To request a native reply/quote on supported surfaces, include one tag in your reply:\n")
	sb.WriteString("- [[reply_to_current]] replies to the triggering message.\n")
	sb.WriteString("Tags are stripped before sending; support depends on the current channel config.\n\n")

	// Messaging.
	sb.WriteString("## Messaging\n")
	sb.WriteString("- Reply in current session → automatically routes to the source channel.\n")
	sb.WriteString("- Cross-session messaging → use sessions_send(sessionKey, message)\n")
	sb.WriteString("- Never use exec/curl for provider messaging; Deneb handles all routing internally.\n")
	if toolSet["message"] {
		sb.WriteString("- Use `message` for proactive sends + channel actions (polls, reactions, etc.).\n")
		sb.WriteString(fmt.Sprintf("- If you use `message` to deliver your user-visible reply, respond with ONLY: %s (avoid duplicate replies).\n", SilentReplyToken))
	}
	sb.WriteString("\n")

	// Current Date & Time.
	tz := params.UserTimezone
	if tz == "" {
		tz = resolveTimezone()
	}
	now := time.Now()
	loc, err := time.LoadLocation(tz)
	if err == nil {
		now = now.In(loc)
	}
	sb.WriteString("## Current Date & Time\n")
	fmt.Fprintf(&sb, "%s\n", now.Format("Monday, January 2, 2006 — 15:04"))
	fmt.Fprintf(&sb, "Time zone: %s\n\n", tz)

	// Context files (CLAUDE.md, SOUL.md, etc.).
	contextPrompt := FormatContextFilesForPrompt(params.ContextFiles)
	if contextPrompt != "" {
		sb.WriteString(contextPrompt)
	}

	// Silent Replies.
	sb.WriteString("## Silent Replies\n")
	fmt.Fprintf(&sb, "When the context makes a reply unnecessary or harmful, reply with ONLY: %s\n", SilentReplyToken)
	sb.WriteString("This suppresses delivery to the user. Use sparingly and only when truly no response is needed.\n\n")

	// Runtime.
	sb.WriteString("## Runtime\n")
	sb.WriteString(buildRuntimeLine(params.RuntimeInfo, params.Channel))
	sb.WriteString("\n")

	return sb.String()
}

// writeToolList writes the formatted tool list to the string builder.
func writeToolList(sb *strings.Builder, defs []ToolDef) {
	registered := make(map[string]string) // name → description
	for _, def := range defs {
		registered[def.Name] = def.Description
	}

	written := make(map[string]bool)

	// Write tools in preferred order first.
	for _, name := range toolOrder {
		desc, ok := registered[name]
		if !ok {
			continue
		}
		// Prefer the more detailed core summary if available.
		if coreSummary, exists := coreToolSummaries[name]; exists {
			desc = coreSummary
		}
		fmt.Fprintf(sb, "- %s: %s\n", name, desc)
		written[name] = true
	}

	// Write any remaining tools not in the preferred order.
	for _, def := range defs {
		if written[def.Name] {
			continue
		}
		desc := def.Description
		if coreSummary, exists := coreToolSummaries[def.Name]; exists {
			desc = coreSummary
		}
		fmt.Fprintf(sb, "- %s: %s\n", def.Name, desc)
	}
}

// buildRuntimeLine constructs the Runtime info line for the system prompt.
func buildRuntimeLine(info *RuntimeInfo, channel string) string {
	parts := []string{"Runtime:"}

	if info != nil {
		if info.AgentID != "" {
			parts = append(parts, fmt.Sprintf("agent=%s", info.AgentID))
		}
		if info.Host != "" {
			parts = append(parts, fmt.Sprintf("host=%s", info.Host))
		}
		if info.OS != "" {
			parts = append(parts, fmt.Sprintf("os=%s(%s)", info.OS, info.Arch))
		}
		if info.Model != "" {
			parts = append(parts, fmt.Sprintf("model=%s", info.Model))
		}
		if info.DefaultModel != "" {
			parts = append(parts, fmt.Sprintf("default_model=%s", info.DefaultModel))
		}
	}

	if channel != "" {
		parts = append(parts, fmt.Sprintf("channel=%s", channel))
	}

	return strings.Join(parts, " ")
}

// resolveTimezone returns the system timezone.
func resolveTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	zone, _ := time.Now().Zone()
	if zone != "" && zone != "UTC" {
		return zone
	}
	return "UTC"
}

// BuildDefaultRuntimeInfo creates RuntimeInfo from the current environment.
func BuildDefaultRuntimeInfo(model, defaultModel string) *RuntimeInfo {
	hostname, _ := os.Hostname()
	return &RuntimeInfo{
		Host:         hostname,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Model:        model,
		DefaultModel: defaultModel,
	}
}
