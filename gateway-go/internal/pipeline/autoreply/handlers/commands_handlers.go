// commands_handlers.go — Command handler types, router, and shared helpers.
//
// Handler implementations are split by domain:
//   - commands_handlers_session.go  — reset, compact, stop/cancel/kill
//   - commands_handlers_model.go    — model, verbose
//   - commands_handlers_info.go     — status
//   - commands_handlers_agents.go   — agents
package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/subagent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
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

// RPCZeroCallsReport holds the result of a zero-calls analysis.
type RPCZeroCallsReport struct {
	ZeroCalls    []string
	TotalMethods int
}

// CommandDeps holds dependencies available to command handlers.
type CommandDeps struct {
	SubagentRuns        func() []subagentpkg.SubagentRunRecord    // for /agents
	Status              *StatusDeps                               // Server-level data for /status command.
	ZeroCallsFn         func() *RPCZeroCallsReport                // for /zerocalls
	MorningLetterDataFn func(ctx context.Context) (string, error) // for /morning — collects raw JSON data
}

// StatusDeps holds server-level data for the /status command.
type StatusDeps struct {
	Version           string
	StartedAt         time.Time
	SessionCount      int
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

	// Status & info
	r.Handle("status", handleStatusCommand)
	r.Handle("agents", handleAgentsCommand)
	r.Handle("help", r.handleHelpCommand)
	r.Handle("commands", r.handleCommandsCommand)

	// Model
	r.Handle("model", handleModelCommand)
	r.Handle("verbose", handleVerboseCommand)

	// Monitoring
	r.Handle("zerocalls", handleZeroCallsCommand)

	// Routine shortcuts (rewrite → agent passthrough)
	r.Handle("morning", handleMorningCommand)
}

func (r *CommandRouter) handleHelpCommand(ctx CommandContext) (*CommandResult, error) {
	commands := r.registry.Commands()
	return &CommandResult{Reply: BuildHelpMessage(commands), SkipAgent: true}, nil
}

func (r *CommandRouter) handleCommandsCommand(ctx CommandContext) (*CommandResult, error) {
	commands := r.registry.Commands()
	page := 0
	if ctx.Args != nil && ctx.Args.Raw != "" {
		if p, err := strconv.Atoi(strings.TrimSpace(ctx.Args.Raw)); err == nil && p >= 0 {
			page = p
		}
	}
	return &CommandResult{Reply: BuildCommandsMessage(commands, page, 20), SkipAgent: true}, nil
}

// --- Helper functions ---

func argRaw(args *CommandArgs) string {
	if args == nil {
		return ""
	}
	return strings.TrimSpace(args.Raw)
}
