// commands_handlers.go — Command handler types, router, and shared helpers.
//
// Handler implementations are split by domain:
//   - commands_handlers_session.go  — session lifecycle, activation, send policy
//   - commands_handlers_model.go    — model, thinking, fast, verbose, reasoning, elevated
//   - commands_handlers_info.go     — status, help, context, info
//   - commands_handlers_config.go   — config, set/unset, system-prompt, debug
//   - commands_handlers_exec.go     — bash, approve, plugins, MCP, allowlist, btw
//   - commands_handlers_agents.go   — agents, spawn, focus, ACP
package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
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
	ModelCandidates []model.ModelCandidate
	BashConfig      BashCommandConfig
	Allowlist       *AllowlistMatcher
	SubagentRuns    func() []*SubagentRunRecord
	McpStore        McpServerStore
	Status          *StatusDeps // Server-level data for /status command.
}

// StatusDeps holds server-level data for the /status command.
type StatusDeps struct {
	Version       string
	StartedAt     time.Time
	RustFFI       bool
	SessionCount  int
	WSConnections int32
	ProviderUsage map[string]*ProviderUsageStats
	ChannelHealth []ChannelHealthEntry
}

// ProviderUsageStats holds per-provider API usage counters.
type ProviderUsageStats struct {
	Calls  int64
	Input  int64
	Output int64
}

// ChannelHealthEntry holds health status for a single channel.
type ChannelHealthEntry struct {
	ID      string
	Healthy bool
	Reason  string
}

// CommandResult holds the outcome of a command execution.
type CommandResult struct {
	Reply      string
	Payloads   []types.ReplyPayload
	SessionMod *types.SessionModification
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
	// Session lifecycle
	r.Handle("new", handleNewCommand)
	r.Handle("reset", handleResetCommand)
	r.Handle("fork", handleForkCommand)
	r.Handle("continue", handleContinueCommand)
	r.Handle("stop", handleStopCommand)
	r.Handle("cancel", handleCancelCommand)
	r.Handle("kill", handleKillCommand)
	r.Handle("compact", handleCompactCommand)
	r.Handle("export", handleExportCommand)
	r.Handle("session", handleSessionLifecycleCommand)
	r.Handle("activation", handleActivationCommand)
	r.Handle("send", handleSendPolicyCommand)

	// Status & info
	r.Handle("status", handleStatusCommand)
	r.Handle("help", handleHelpCommand)
	r.Handle("context", handleContextCommand)
	r.Handle("info", handleInfoCommand)
	r.Handle("usage", handleUsageCommand)

	// Model & thinking
	r.Handle("model", handleModelCommand)
	r.Handle("models", handleModelsListCommand)
	r.Handle("think", handleThinkCommand)
	r.Handle("fast", handleFastCommand)
	r.Handle("verbose", handleVerboseCommand)
	r.Handle("reasoning", handleReasoningCommand)
	r.Handle("elevated", handleElevatedCommand)

	// Config
	r.Handle("config", handleConfigCommand)
	r.Handle("set", handleSetCommand)
	r.Handle("unset", handleUnsetCommand)
	r.Handle("system-prompt", handleSystemPromptCommand)
	r.Handle("debug", handleDebugCommand)

	// Execution & tools
	r.Handle("bash", handleBashCommand)
	r.Handle("approve", handleApproveCommand)
	r.Handle("plugins", handlePluginsCommand)
	r.Handle("plugin", handlePluginCommand)
	r.Handle("mcp", handleMCPCommand)
	r.Handle("allowlist", handleAllowlistCommand)
	r.Handle("btw", handleBtwCommand)

	// Subagents
	r.Handle("agents", handleAgentsCommand)
	r.Handle("agent", handleAgentCommand)
	r.Handle("spawn", handleSpawnCommand)
	r.Handle("focus", handleFocusCommand)
	r.Handle("unfocus", handleUnfocusCommand)
	r.Handle("acp", handleACPCommand)
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
