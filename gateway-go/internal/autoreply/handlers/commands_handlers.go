// commands_handlers.go — Command handler types, router, and shared helpers.
//
// Handler implementations are split by domain:
//   - commands_handlers_session.go  — reset, compact, stop/cancel/kill
//   - commands_handlers_model.go    — model, verbose
//   - commands_handlers_info.go     — status
//   - commands_handlers_agents.go   — agents
package handlers

import (
	"fmt"
	"strings"
	"time"

	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/autoreply/subagent"
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
	SubagentRuns    func() []subagentpkg.SubagentRunRecord // for /agents
	Status          *StatusDeps                            // Server-level data for /status command.
}

// StatusDeps holds server-level data for the /status command.
type StatusDeps struct {
	Version           string
	StartedAt         time.Time
	RustFFI           bool
	SessionCount      int
	WSConnections     int32
	ProviderUsage     map[string]*ProviderUsageStats
	ChannelHealth     []ChannelHealthEntry
	LastFailureReason string // reason the most recent run failed, if any
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
	// Session
	r.Handle("reset", handleResetCommand)
	r.Handle("stop", handleStopCommand)
	r.Handle("cancel", handleCancelCommand)
	r.Handle("kill", handleKillCommand)
	r.Handle("compact", handleCompactCommand)

	// Status
	r.Handle("status", handleStatusCommand)
	r.Handle("agents", handleAgentsCommand)

	// Model
	r.Handle("model", handleModelCommand)
	r.Handle("verbose", handleVerboseCommand)
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
