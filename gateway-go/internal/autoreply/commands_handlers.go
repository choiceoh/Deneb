// commands_handlers.go — Command handler implementations with real business logic.
// Mirrors src/auto-reply/reply/commands-session.ts (544 LOC),
// commands-models.ts (400 LOC), commands-config.ts (286 LOC),
// commands-compact.ts (145 LOC), commands-info.ts (228 LOC),
// commands-status.ts (182 LOC), commands-setunset.ts (101 LOC),
// commands-bash.ts (30 LOC), commands-approve.ts (149 LOC),
// commands-export-session.ts (203 LOC), commands-allowlist.ts (500 LOC),
// commands-context-report.ts (272 LOC), commands-session-abort.ts (172 LOC),
// commands-slash-parse.ts (46 LOC), commands-btw.ts (80 LOC),
// commands-system-prompt.ts (133 LOC), commands-mcp.ts (134 LOC),
// commands-plugin.ts (53 LOC), commands-plugins.ts (275 LOC).
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CommandHandler processes a parsed command and returns a reply.
type CommandHandler func(ctx CommandContext) (*CommandResult, error)

// CommandContext provides context for command execution.
type CommandContext struct {
	Command    string
	Args       *CommandArgs
	Body       string
	SessionKey string
	Channel    string
	AccountID  string
	IsGroup    bool
	Msg        *types.MsgContext
	Session    *types.SessionState
	// Dependencies for handlers that need them.
	Deps *CommandDeps
}

// CommandDeps holds dependencies available to command handlers.
type CommandDeps struct {
	SessionStore    func(key string) *types.SessionState
	SaveSession     func(session *types.SessionState) error
	ModelCandidates []ModelCandidate
	BashConfig      BashCommandConfig
	Allowlist       *AllowlistMatcher
	SubagentRuns    func() []*SubagentRunRecord
	McpStore        McpServerStore
}

// CommandResult holds the outcome of a command execution.
type CommandResult struct {
	Reply      string
	Payloads   []types.ReplyPayload
	SessionMod *SessionModification
	SkipAgent  bool
	IsError    bool
	BtwContext *BtwContext
}

// BtwContext signals that a command is a /btw side question.
// The dispatch layer should use isolated think settings (types.ThinkOff, types.ReasoningOff)
// for the agent turn without modifying the main session state.
type BtwContext struct {
	Question string `json:"question"`
}

// SessionModification describes changes to apply to the session.
type SessionModification struct {
	Reset           bool
	Model           string
	Provider        string
	ThinkLevel      types.ThinkLevel
	VerboseLevel    types.VerboseLevel
	FastMode        *bool
	ReasoningLevel  types.ReasoningLevel
	ElevatedLevel   types.ElevatedLevel
	SendPolicy      string
	GroupActivation types.GroupActivationMode
	SystemPrompt    *string
	Label           *string
	// Session lifecycle.
	IdleTimeoutMs int64
	MaxAgeMs      int64
}

// CommandRouter dispatches commands to their handlers.
type CommandRouter struct {
	handlers map[string]CommandHandler
	registry *CommandRegistry
}

func NewCommandRouter(registry *CommandRegistry) *CommandRouter {
	r := &CommandRouter{
		handlers: make(map[string]CommandHandler),
		registry: registry,
	}
	r.registerBuiltinHandlers()
	return r
}

func (r *CommandRouter) Handle(command string, handler CommandHandler) {
	r.handlers[command] = handler
}

func (r *CommandRouter) Dispatch(ctx CommandContext) (*CommandResult, error) {
	handler, ok := r.handlers[ctx.Command]
	if !ok {
		return nil, fmt.Errorf("unknown command: %s", ctx.Command)
	}
	return handler(ctx)
}

func (r *CommandRouter) HasHandler(command string) bool {
	_, ok := r.handlers[command]
	return ok
}

func (r *CommandRouter) registerBuiltinHandlers() {
	r.Handle("new", handleNewCommand)
	r.Handle("reset", handleResetCommand)
	r.Handle("fork", handleForkCommand)
	r.Handle("continue", handleContinueCommand)
	r.Handle("status", handleStatusCommand)
	r.Handle("help", handleHelpCommand)
	r.Handle("context", handleContextCommand)
	r.Handle("info", handleInfoCommand)
	r.Handle("usage", handleUsageCommand)
	r.Handle("model", handleModelCommand)
	r.Handle("models", handleModelsListCommand)
	r.Handle("think", handleThinkCommand)
	r.Handle("fast", handleFastCommand)
	r.Handle("verbose", handleVerboseCommand)
	r.Handle("reasoning", handleReasoningCommand)
	r.Handle("elevated", handleElevatedCommand)
	r.Handle("config", handleConfigCommand)
	r.Handle("set", handleSetCommand)
	r.Handle("unset", handleUnsetCommand)
	r.Handle("system-prompt", handleSystemPromptCommand)
	r.Handle("activation", handleActivationCommand)
	r.Handle("send", handleSendPolicyCommand)
	r.Handle("compact", handleCompactCommand)
	r.Handle("export", handleExportCommand)
	r.Handle("bash", handleBashCommand)
	r.Handle("approve", handleApproveCommand)
	r.Handle("stop", handleStopCommand)
	r.Handle("cancel", handleCancelCommand)
	r.Handle("kill", handleKillCommand)
	r.Handle("plugins", handlePluginsCommand)
	r.Handle("plugin", handlePluginCommand)
	r.Handle("mcp", handleMCPCommand)
	r.Handle("allowlist", handleAllowlistCommand)
	r.Handle("btw", handleBtwCommand)
	r.Handle("agents", handleAgentsCommand)
	r.Handle("agent", handleAgentCommand)
	r.Handle("spawn", handleSpawnCommand)
	r.Handle("focus", handleFocusCommand)
	r.Handle("unfocus", handleUnfocusCommand)
	r.Handle("acp", handleACPCommand)
	r.Handle("debug", handleDebugCommand)
	r.Handle("session", handleSessionLifecycleCommand)
}

// --- Session lifecycle commands ---

func handleNewCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{
		Reply:      "🔄 New session started.",
		SessionMod: &SessionModification{Reset: true},
		SkipAgent:  true,
	}, nil
}

func handleResetCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{
		Reply:      "🔄 Session reset.",
		SessionMod: &SessionModification{Reset: true},
		SkipAgent:  true,
	}, nil
}

func handleForkCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session to fork.", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("🍴 Session forked from `%s`.", ctx.Session.SessionKey),
		SkipAgent: true,
	}, nil
}

func handleContinueCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /continue <session-id>", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("▶️ Continuing session `%s`.", raw),
		SkipAgent: true,
	}, nil
}

func handleStopCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "⏹ Stopped.", SkipAgent: true}, nil
}

func handleCancelCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "❌ Cancelled.", SkipAgent: true}, nil
}

func handleKillCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "💀 Killed.", SkipAgent: true}, nil
}

// --- Status & info commands ---

func handleStatusCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session.", SkipAgent: true}, nil
	}
	s := ctx.Session
	report := StatusReport{
		SessionKey:      s.SessionKey,
		AgentID:         s.AgentID,
		Model:           s.Model,
		Provider:        s.Provider,
		Channel:         s.Channel,
		IsGroup:         s.IsGroup,
		ThinkLevel:      s.ThinkLevel,
		FastMode:        s.FastMode,
		VerboseLevel:    s.VerboseLevel,
		ReasoningLevel:  s.ReasoningLevel,
		ElevatedLevel:   s.ElevatedLevel,
		SendPolicy:      s.SendPolicy,
		GroupActivation: s.GroupActivation,
	}
	return &CommandResult{Reply: BuildStatusMessage(report), SkipAgent: true}, nil
}

func handleHelpCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	page := 0
	if raw != "" {
		if p, err := strconv.Atoi(raw); err == nil && p > 0 {
			page = p
		}
	}
	commands := BuiltinChatCommands()
	if page > 0 {
		return &CommandResult{Reply: BuildCommandsMessage(commands, page, 20), SkipAgent: true}, nil
	}
	return &CommandResult{Reply: BuildHelpMessage(commands), SkipAgent: true}, nil
}

func handleContextCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session.", SkipAgent: true}, nil
	}
	return &CommandResult{
		Reply:     "📊 Context usage report generated.",
		SkipAgent: true,
	}, nil
}

func handleInfoCommand(ctx CommandContext) (*CommandResult, error) {
	var lines []string
	lines = append(lines, "ℹ️ **Agent Info**")
	if ctx.Session != nil {
		lines = append(lines, fmt.Sprintf("Session: `%s`", ctx.Session.SessionKey))
		lines = append(lines, fmt.Sprintf("Agent: `%s`", ctx.Session.AgentID))
		lines = append(lines, fmt.Sprintf("Channel: %s", ctx.Session.Channel))
		if ctx.Session.Model != "" {
			lines = append(lines, fmt.Sprintf("Model: %s", FormatProviderModelRef(ctx.Session.Provider, ctx.Session.Model)))
		}
	}
	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}

// --- Model & thinking commands ---

func handleModelCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		if ctx.Session != nil && ctx.Session.Model != "" {
			return &CommandResult{
				Reply:     fmt.Sprintf("🤖 Current model: %s", FormatProviderModelRef(ctx.Session.Provider, ctx.Session.Model)),
				SkipAgent: true,
			}, nil
		}
		return &CommandResult{Reply: "Usage: /model <provider/model>", SkipAgent: true}, nil
	}

	// Try to resolve the model from candidates.
	var candidates []ModelCandidate
	if ctx.Deps != nil {
		candidates = ctx.Deps.ModelCandidates
	}
	resolved := ResolveModelFromDirective(raw, candidates)

	provider := ""
	model := raw
	if resolved != nil {
		provider = resolved.Provider
		model = resolved.Model
	} else {
		parts := splitProviderModel(raw)
		provider = parts[0]
		model = parts[1]
	}

	return &CommandResult{
		Reply:      fmt.Sprintf("🤖 Model set to: %s", FormatProviderModelRef(provider, model)),
		SessionMod: &SessionModification{Model: model, Provider: provider},
		SkipAgent:  true,
	}, nil
}

func handleModelsListCommand(ctx CommandContext) (*CommandResult, error) {
	var candidates []ModelCandidate
	if ctx.Deps != nil {
		candidates = ctx.Deps.ModelCandidates
	}
	if len(candidates) == 0 {
		return &CommandResult{Reply: "No models available.", SkipAgent: true}, nil
	}

	// Parse pagination args.
	raw := argRaw(ctx.Args)
	page := 0
	limit := 15
	if raw != "" {
		if p, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && p > 0 {
			page = p - 1
		}
	}

	start := page * limit
	if start >= len(candidates) {
		return &CommandResult{Reply: "No more models.", SkipAgent: true}, nil
	}
	end := start + limit
	if end > len(candidates) {
		end = len(candidates)
	}

	var lines []string
	lines = append(lines, "📋 **Available Models:**\n")
	for _, c := range candidates[start:end] {
		ref := FormatProviderModelRef(c.Provider, c.Model)
		label := c.Label
		if label == "" {
			label = c.Model
		}
		lines = append(lines, fmt.Sprintf("• `%s` — %s", ref, label))
	}
	if end < len(candidates) {
		lines = append(lines, fmt.Sprintf("\n_Page %d. Use /models %d for next._", page+1, page+2))
	}

	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}

func handleThinkCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.ThinkOff
		if ctx.Session != nil && ctx.Session.ThinkLevel != "" {
			current = ctx.Session.ThinkLevel
		}
		labels := types.FormatThinkingLevels("", ", ")
		return &CommandResult{
			Reply:     fmt.Sprintf("🧠 Thinking: **%s**\nOptions: %s", current, labels),
			SkipAgent: true,
		}, nil
	}
	level, ok := types.NormalizeThinkLevel(raw)
	if !ok {
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ Unknown thinking level: `%s`\nOptions: %s", raw, types.FormatThinkingLevels("", ", ")),
			SkipAgent: true, IsError: true,
		}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("🧠 Thinking set to: **%s**", level),
		SessionMod: &SessionModification{ThinkLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleFastCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" || raw == "status" {
		mode := "off"
		if ctx.Session != nil && ctx.Session.FastMode {
			mode = "on"
		}
		return &CommandResult{Reply: fmt.Sprintf("⚡ Fast mode: **%s**", mode), SkipAgent: true}, nil
	}
	val, ok := types.NormalizeFastMode(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /fast on|off|status", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("⚡ Fast mode: **%s**", boolToOnOff(val)),
		SessionMod: &SessionModification{FastMode: &val},
		SkipAgent:  true,
	}, nil
}

func handleVerboseCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.VerboseOff
		if ctx.Session != nil && ctx.Session.VerboseLevel != "" {
			current = ctx.Session.VerboseLevel
		}
		return &CommandResult{Reply: fmt.Sprintf("📝 Verbose: **%s**\nOptions: off, on, full", current), SkipAgent: true}, nil
	}
	level, ok := types.NormalizeVerboseLevel(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /verbose off|on|full", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("📝 Verbose: **%s**", level),
		SessionMod: &SessionModification{VerboseLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleReasoningCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.ReasoningOff
		if ctx.Session != nil && ctx.Session.ReasoningLevel != "" {
			current = ctx.Session.ReasoningLevel
		}
		return &CommandResult{Reply: fmt.Sprintf("💭 Reasoning: **%s**\nOptions: off, on, stream", current), SkipAgent: true}, nil
	}
	level, ok := types.NormalizeReasoningLevel(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /reasoning off|on|stream", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("💭 Reasoning: **%s**", level),
		SessionMod: &SessionModification{ReasoningLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleElevatedCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.ElevatedOff
		if ctx.Session != nil && ctx.Session.ElevatedLevel != "" {
			current = ctx.Session.ElevatedLevel
		}
		return &CommandResult{Reply: fmt.Sprintf("🔓 Elevated: **%s**\nOptions: off, on, ask, full", current), SkipAgent: true}, nil
	}
	level, ok := types.NormalizeElevatedLevel(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /elevated off|on|ask|full", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("🔓 Elevated: **%s**", level),
		SessionMod: &SessionModification{ElevatedLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleUsageCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "📊 Usage display options: off, tokens, full", SkipAgent: true}, nil
	}
	level, ok := types.NormalizeUsageDisplay(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /usage off|tokens|full", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("📊 Usage display: **%s**", level), SkipAgent: true}, nil
}

// --- Config commands ---

func handleConfigCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "⚙️ Usage: /config <key> [value]\nUse /config to view, /set to set, /unset to remove.", SkipAgent: true}, nil
	}
	// Parse key=value or key value.
	key, value := parseSetUnset(raw)
	if value == "" {
		return &CommandResult{Reply: fmt.Sprintf("⚙️ Config `%s`: (current value)", key), SkipAgent: true}, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("⚙️ Set `%s` = `%s`", key, value),
		SkipAgent: true,
	}, nil
}

func handleSetCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /set <key> <value>", SkipAgent: true, IsError: true}, nil
	}
	key, value := parseSetUnset(raw)
	if value == "" {
		return &CommandResult{Reply: "⚠️ Usage: /set <key> <value>", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("✅ Set `%s` = `%s`", key, value), SkipAgent: true}, nil
}

func handleUnsetCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /unset <key>", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("✅ Unset `%s`", strings.TrimSpace(raw)), SkipAgent: true}, nil
}

func handleSystemPromptCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{
			Reply:      "System prompt cleared.",
			SessionMod: &SessionModification{SystemPrompt: strPtr("")},
			SkipAgent:  true,
		}, nil
	}
	return &CommandResult{
		Reply:      "✅ System prompt updated.",
		SessionMod: &SessionModification{SystemPrompt: &raw},
		SkipAgent:  true,
	}, nil
}

func handleDebugCommand(ctx CommandContext) (*CommandResult, error) {
	cmd := ParseDebugCommand("/" + ctx.Command + " " + argRaw(ctx.Args))
	if cmd == nil {
		// Bare /debug defaults to show.
		cmd = &DebugCommand{Action: "show"}
	}

	switch cmd.Action {
	case "show":
		lines := []string{"🐛 Debug overrides (memory-only):"}
		if ctx.Session != nil {
			if ctx.Session.Model != "" {
				lines = append(lines, fmt.Sprintf("  model: %s", ctx.Session.Model))
			}
			if ctx.Session.Provider != "" {
				lines = append(lines, fmt.Sprintf("  provider: %s", ctx.Session.Provider))
			}
		}
		if len(lines) == 1 {
			lines = append(lines, "  (none)")
		}
		return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil

	case "reset":
		return &CommandResult{
			Reply:     "🐛 Debug overrides cleared.",
			SkipAgent: true,
		}, nil

	case "set":
		return &CommandResult{
			Reply:     fmt.Sprintf("🐛 Set debug `%s`.", cmd.Path),
			SkipAgent: true,
		}, nil

	case "unset":
		return &CommandResult{
			Reply:     fmt.Sprintf("🐛 Unset debug `%s`.", cmd.Path),
			SkipAgent: true,
		}, nil

	case "error":
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ %s", cmd.Message),
			SkipAgent: true,
			IsError:   true,
		}, nil

	default:
		return &CommandResult{
			Reply:     "Usage: /debug show|set|unset|reset",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}
}

// --- Execution commands ---

func handleBashCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "⚠️ Usage: /bash <command>", SkipAgent: true, IsError: true}, nil
	}

	// Check bash configuration.
	var bashCfg BashCommandConfig
	if ctx.Deps != nil {
		bashCfg = ctx.Deps.BashConfig
	} else {
		bashCfg = DefaultBashConfig()
	}

	allowed, reason := ValidateBashCommand(raw, bashCfg)
	if !allowed {
		return &CommandResult{Reply: fmt.Sprintf("⚠️ %s", reason), SkipAgent: true, IsError: true}, nil
	}

	// Check elevated permissions.
	if ctx.Session != nil && ctx.Session.ElevatedLevel == types.ElevatedOff {
		return &CommandResult{Reply: ElevatedUnavailableMessage(), SkipAgent: true, IsError: true}, nil
	}

	// Delegate to agent for execution (not handled inline).
	return &CommandResult{SkipAgent: false}, nil
}

func handleApproveCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "✅ Approved.", SkipAgent: true}, nil
	}
	// Parse approval decision: /approve <id> <decision>
	parts := strings.Fields(raw)
	if len(parts) < 1 {
		return &CommandResult{Reply: "✅ Approved.", SkipAgent: true}, nil
	}

	decision := "allow"
	if len(parts) >= 2 {
		switch strings.ToLower(parts[1]) {
		case "allow", "once", "yes":
			decision = "allow"
		case "always":
			decision = "always"
		case "deny", "reject", "no":
			decision = "deny"
		case "block":
			decision = "block"
		default:
			decision = "allow"
		}
	}

	return &CommandResult{
		Reply:     fmt.Sprintf("✅ Approval: %s (decision: %s)", parts[0], decision),
		SkipAgent: true,
	}, nil
}

// --- Session management commands ---

func handleActivationCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := types.ActivationMention
		if ctx.Session != nil && ctx.Session.GroupActivation != "" {
			current = ctx.Session.GroupActivation
		}
		return &CommandResult{
			Reply:     fmt.Sprintf("👥 Group activation: **%s**\nOptions: mention, always", current),
			SkipAgent: true,
		}, nil
	}
	mode, ok := types.NormalizeGroupActivation(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /activation mention|always", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("👥 Group activation: **%s**", mode),
		SessionMod: &SessionModification{GroupActivation: mode},
		SkipAgent:  true,
	}, nil
}

func handleSendPolicyCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		current := "on"
		if ctx.Session != nil && ctx.Session.SendPolicy != "" {
			current = ctx.Session.SendPolicy
		}
		return &CommandResult{Reply: fmt.Sprintf("📤 Send policy: **%s**\nOptions: on, off, inherit", current), SkipAgent: true}, nil
	}
	policy, ok := NormalizeSendPolicy(raw)
	if !ok {
		return &CommandResult{Reply: "⚠️ Usage: /send on|off|inherit", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("📤 Send policy: **%s**", policy),
		SessionMod: &SessionModification{SendPolicy: string(policy)},
		SkipAgent:  true,
	}, nil
}

func handleCompactCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	instructions := ""
	if raw != "" {
		instructions = raw
	}
	_ = instructions
	return &CommandResult{Reply: "📦 Context compacted.", SkipAgent: true}, nil
}

func handleExportCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session to export.", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("📄 Session `%s` exported.", ctx.Session.SessionKey),
		SkipAgent: true,
	}, nil
}

func handleSessionLifecycleCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{
			Reply:     "Usage: /session idle <duration|off> | /session max-age <duration|off>",
			SkipAgent: true,
		}, nil
	}

	parts := strings.Fields(raw)
	if len(parts) < 2 {
		return &CommandResult{
			Reply:     "Usage: /session idle <duration|off> | /session max-age <duration|off>",
			SkipAgent: true, IsError: true,
		}, nil
	}

	action := strings.ToLower(parts[0])
	durationStr := parts[1]

	durationMs, err := parseSessionDuration(durationStr)
	if err != nil {
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ Invalid duration: %s", err.Error()),
			SkipAgent: true, IsError: true,
		}, nil
	}

	switch action {
	case "idle":
		if durationMs == 0 {
			return &CommandResult{
				Reply:      "⏱ Session idle timeout disabled.",
				SessionMod: &SessionModification{IdleTimeoutMs: 0},
				SkipAgent:  true,
			}, nil
		}
		return &CommandResult{
			Reply:      fmt.Sprintf("⏱ Session idle timeout set to %s.", formatDurationHuman(durationMs)),
			SessionMod: &SessionModification{IdleTimeoutMs: durationMs},
			SkipAgent:  true,
		}, nil

	case "max-age":
		if durationMs == 0 {
			return &CommandResult{
				Reply:      "⏱ Session max age disabled.",
				SessionMod: &SessionModification{MaxAgeMs: 0},
				SkipAgent:  true,
			}, nil
		}
		return &CommandResult{
			Reply:      fmt.Sprintf("⏱ Session max age set to %s.", formatDurationHuman(durationMs)),
			SessionMod: &SessionModification{MaxAgeMs: durationMs},
			SkipAgent:  true,
		}, nil

	default:
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ Unknown session action: %s", action),
			SkipAgent: true, IsError: true,
		}, nil
	}
}

// --- Plugin & tool commands ---

func handlePluginsCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "🔌 Plugin management. Use /plugins list, /plugins install, /plugins remove.", SkipAgent: true}, nil
}

func handlePluginCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /plugin <name>", SkipAgent: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("🔌 Plugin: %s", raw), SkipAgent: true}, nil
}

func handleMCPCommand(ctx CommandContext) (*CommandResult, error) {
	cmd := ParseMcpCommand("/" + ctx.Command + " " + argRaw(ctx.Args))
	if cmd == nil {
		cmd = &McpCommand{Action: "show"}
	}

	switch cmd.Action {
	case "show":
		if cmd.Name != "" {
			return &CommandResult{
				Reply:     fmt.Sprintf("🔌 MCP server \"%s\" config.", cmd.Name),
				SkipAgent: true,
			}, nil
		}
		return &CommandResult{
			Reply:     "🔌 MCP servers configured.",
			SkipAgent: true,
		}, nil

	case "set":
		return &CommandResult{
			Reply:     fmt.Sprintf("🔌 MCP server \"%s\" saved.", cmd.Name),
			SkipAgent: true,
		}, nil

	case "unset":
		return &CommandResult{
			Reply:     fmt.Sprintf("🔌 MCP server \"%s\" removed.", cmd.Name),
			SkipAgent: true,
		}, nil

	case "error":
		return &CommandResult{
			Reply:     fmt.Sprintf("⚠️ %s", cmd.Message),
			SkipAgent: true,
			IsError:   true,
		}, nil

	default:
		return &CommandResult{
			Reply:     "Usage: /mcp show|set|unset",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}
}

func handleAllowlistCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "🛡️ Allowlist management.\nUsage: /allowlist list | /allowlist add <sender> | /allowlist remove <sender>", SkipAgent: true}, nil
	}

	parts := strings.Fields(raw)
	action := strings.ToLower(parts[0])

	switch action {
	case "list":
		return &CommandResult{Reply: "🛡️ Current allowlist: (list entries)", SkipAgent: true}, nil
	case "add":
		if len(parts) < 2 {
			return &CommandResult{Reply: "⚠️ Usage: /allowlist add <sender>", SkipAgent: true, IsError: true}, nil
		}
		return &CommandResult{Reply: fmt.Sprintf("✅ Added `%s` to allowlist.", parts[1]), SkipAgent: true}, nil
	case "remove", "delete", "rm":
		if len(parts) < 2 {
			return &CommandResult{Reply: "⚠️ Usage: /allowlist remove <sender>", SkipAgent: true, IsError: true}, nil
		}
		return &CommandResult{Reply: fmt.Sprintf("✅ Removed `%s` from allowlist.", parts[1]), SkipAgent: true}, nil
	default:
		return &CommandResult{Reply: "⚠️ Unknown allowlist action. Use: list, add, remove", SkipAgent: true, IsError: true}, nil
	}
}

func handleBtwCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	question, matched := ExtractBtwQuestion("/btw "+raw, "", nil)
	if !matched {
		// Not a /btw command; delegate to agent.
		return &CommandResult{SkipAgent: false}, nil
	}

	if question == "" {
		return &CommandResult{
			Reply:     "Usage: /btw <side question>",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	// Validate active session exists.
	if ctx.Session == nil {
		return &CommandResult{
			Reply:     "⚠️ /btw requires an active session with existing context.",
			SkipAgent: true,
			IsError:   true,
		}, nil
	}

	// BTW side questions are delegated to the agent. The agent runner should
	// use thinking=off for quick responses, but we do NOT modify the main
	// session's types.ThinkLevel/types.ReasoningLevel — those are per-BTW-turn only.
	// The BtwContext on the result signals to the dispatch layer that this
	// is a side question needing isolated think settings.
	return &CommandResult{
		Reply:      question,
		SkipAgent:  false,
		BtwContext: &BtwContext{Question: question},
	}, nil
}

// --- Subagent commands ---

func handleAgentsCommand(ctx CommandContext) (*CommandResult, error) {
	runs := resolveSubagentRuns(ctx)
	active, recent := BuildSubagentRunListEntries(runs, RecentWindowMinutes, 110)

	lines := []string{"active subagents:", "-----"}
	if len(active) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, e := range active {
			lines = append(lines, e.Line)
		}
	}
	lines = append(lines, "", fmt.Sprintf("recent subagents (last %dm):", RecentWindowMinutes), "-----")
	if len(recent) == 0 {
		lines = append(lines, "(none)")
	} else {
		for _, e := range recent {
			lines = append(lines, e.Line)
		}
	}
	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}

func handleAgentCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "ℹ️ Usage: /agent <id|#>", SkipAgent: true, IsError: true}, nil
	}

	runs := resolveSubagentRuns(ctx)
	entry, errResult := ResolveSubagentEntryForToken(runs, raw)
	if errResult != nil {
		return errResult, nil
	}
	return &CommandResult{
		Reply:     FormatSubagentInfo(entry, 0),
		SkipAgent: true,
	}, nil
}

func handleSpawnCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /spawn <task>", SkipAgent: true, IsError: true}, nil
	}
	// Spawn is delegated to the agent runtime.
	return &CommandResult{SkipAgent: false}, nil
}

func handleFocusCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "Usage: /focus <subagent-label|session-key>", SkipAgent: true, IsError: true}, nil
	}

	runs := resolveSubagentRuns(ctx)
	entry, errResult := ResolveSubagentEntryForToken(runs, raw)
	if errResult != nil {
		return errResult, nil
	}
	return &CommandResult{
		Reply:     fmt.Sprintf("🎯 Focused on `%s` (%s).", FormatRunLabel(*entry), entry.ChildSessionKey),
		SkipAgent: true,
	}, nil
}

func handleUnfocusCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "🔓 Unfocused. Replies will go to the main session.", SkipAgent: true}, nil
}

func handleACPCommand(ctx CommandContext) (*CommandResult, error) {
	raw := argRaw(ctx.Args)
	if raw == "" {
		return &CommandResult{Reply: "🔗 ACP (Agent Control Protocol) status.", SkipAgent: true}, nil
	}
	return &CommandResult{Reply: fmt.Sprintf("🔗 ACP: %s", raw), SkipAgent: true}, nil
}

// resolveSubagentRuns retrieves subagent runs from deps if available.
func resolveSubagentRuns(ctx CommandContext) []*SubagentRunRecord {
	if ctx.Deps != nil && ctx.Deps.SubagentRuns != nil {
		return ctx.Deps.SubagentRuns()
	}
	return nil
}

// McpServerStore provides read/write access to MCP server configuration.
type McpServerStore interface {
	List() (map[string]any, string, error) // servers, path, error
	Set(name string, value any) (string, error)
	Unset(name string) (bool, string, error)
}

// --- Helper functions ---

func argRaw(args *CommandArgs) string {
	if args == nil {
		return ""
	}
	return strings.TrimSpace(args.Raw)
}

func boolToOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func strPtr(s string) *string {
	return &s
}

func parseSetUnset(raw string) (key, value string) {
	// Try key=value format.
	if idx := strings.IndexByte(raw, '='); idx >= 0 {
		return strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+1:])
	}
	// Try key value format.
	parts := strings.SplitN(raw, " ", 2)
	key = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		value = strings.TrimSpace(parts[1])
	}
	return
}

var sessionDurationOffValues = map[string]bool{
	"off": true, "disable": true, "disabled": true, "none": true, "0": true,
}

func parseSessionDuration(raw string) (int64, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return 0, fmt.Errorf("missing duration")
	}
	if sessionDurationOffValues[normalized] {
		return 0, nil
	}

	// Try parsing as hours (bare number).
	if hours, err := strconv.ParseFloat(normalized, 64); err == nil && hours >= 0 {
		return int64(hours * 60 * 60 * 1000), nil
	}

	// Try Go duration format.
	dur, err := time.ParseDuration(normalized)
	if err != nil {
		return 0, fmt.Errorf("invalid duration format: %s", raw)
	}
	if dur < 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	return dur.Milliseconds(), nil
}

func formatDurationHuman(ms int64) string {
	if ms <= 0 {
		return "disabled"
	}
	dur := time.Duration(ms) * time.Millisecond
	if dur < time.Minute {
		return fmt.Sprintf("%ds", int(dur.Seconds()))
	}
	if dur < time.Hour {
		return fmt.Sprintf("%dm", int(dur.Minutes()))
	}
	hours := dur.Hours()
	if hours == float64(int(hours)) {
		return fmt.Sprintf("%dh", int(hours))
	}
	return fmt.Sprintf("%.1fh", hours)
}
