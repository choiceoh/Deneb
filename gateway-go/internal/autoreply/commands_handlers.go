package autoreply

import (
	"fmt"
	"strings"
)

// CommandHandler processes a parsed command and returns a reply.
type CommandHandler func(ctx CommandContext) (*CommandResult, error)

// CommandContext provides context for command execution.
type CommandContext struct {
	Command     string // normalized command key (e.g., "new", "model")
	Args        *CommandArgs
	Body        string // full message body
	SessionKey  string
	Channel     string
	IsGroup     bool
	Msg         *MsgContext
	Session     *SessionState
}

// CommandResult holds the outcome of a command execution.
type CommandResult struct {
	Reply       string
	Payloads    []ReplyPayload
	SessionMod  *SessionModification
	SkipAgent   bool // true if the command fully handles the reply
	IsError     bool
}

// SessionModification describes changes to apply to the session.
type SessionModification struct {
	Reset          bool
	Model          string
	ThinkLevel     ThinkLevel
	VerboseLevel   VerboseLevel
	FastMode       *bool
	ReasoningLevel ReasoningLevel
	ElevatedLevel  ElevatedLevel
	SendPolicy     string
	GroupActivation GroupActivationMode
}

// CommandRouter dispatches commands to their handlers.
type CommandRouter struct {
	handlers map[string]CommandHandler
	registry *CommandRegistry
}

// NewCommandRouter creates a new command router.
func NewCommandRouter(registry *CommandRegistry) *CommandRouter {
	r := &CommandRouter{
		handlers: make(map[string]CommandHandler),
		registry: registry,
	}
	r.registerBuiltinHandlers()
	return r
}

// Handle registers a command handler.
func (r *CommandRouter) Handle(command string, handler CommandHandler) {
	r.handlers[command] = handler
}

// Dispatch routes a command to its handler.
func (r *CommandRouter) Dispatch(ctx CommandContext) (*CommandResult, error) {
	handler, ok := r.handlers[ctx.Command]
	if !ok {
		return nil, fmt.Errorf("unknown command: %s", ctx.Command)
	}
	return handler(ctx)
}

// HasHandler returns true if a handler exists for the command.
func (r *CommandRouter) HasHandler(command string) bool {
	_, ok := r.handlers[command]
	return ok
}

func (r *CommandRouter) registerBuiltinHandlers() {
	r.Handle("new", handleNewCommand)
	r.Handle("reset", handleResetCommand)
	r.Handle("status", handleStatusCommand)
	r.Handle("model", handleModelCommand)
	r.Handle("think", handleThinkCommand)
	r.Handle("fast", handleFastCommand)
	r.Handle("verbose", handleVerboseCommand)
	r.Handle("reasoning", handleReasoningCommand)
	r.Handle("elevated", handleElevatedCommand)
	r.Handle("activation", handleActivationCommand)
	r.Handle("send", handleSendPolicyCommand)
	r.Handle("usage", handleUsageCommand)
	r.Handle("compact", handleCompactCommand)
	r.Handle("export", handleExportCommand)
	r.Handle("context", handleContextCommand)
	r.Handle("help", handleHelpCommand)
}

// --- Built-in command handlers ---

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

func handleStatusCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session.", SkipAgent: true}, nil
	}
	s := ctx.Session
	var lines []string
	lines = append(lines, fmt.Sprintf("**Session:** %s", s.SessionKey))
	if s.Model != "" {
		lines = append(lines, fmt.Sprintf("**Model:** %s/%s", s.Provider, s.Model))
	}
	if s.ThinkLevel != "" && s.ThinkLevel != ThinkOff {
		lines = append(lines, fmt.Sprintf("**Thinking:** %s", s.ThinkLevel))
	}
	if s.FastMode {
		lines = append(lines, "**Fast mode:** on")
	}
	if s.VerboseLevel != "" && s.VerboseLevel != VerboseOff {
		lines = append(lines, fmt.Sprintf("**Verbose:** %s", s.VerboseLevel))
	}
	return &CommandResult{Reply: strings.Join(lines, "\n"), SkipAgent: true}, nil
}

func handleModelCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	if raw == "" {
		return &CommandResult{
			Reply:     "Usage: /model <provider/model>",
			SkipAgent: true,
		}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("Model set to: %s", raw),
		SessionMod: &SessionModification{Model: raw},
		SkipAgent:  true,
	}, nil
}

func handleThinkCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	level, ok := NormalizeThinkLevel(raw)
	if !ok && raw == "" {
		// Toggle: show current level.
		if ctx.Session != nil && ctx.Session.ThinkLevel != "" {
			return &CommandResult{
				Reply:     fmt.Sprintf("Thinking level: %s", ctx.Session.ThinkLevel),
				SkipAgent: true,
			}, nil
		}
		return &CommandResult{
			Reply:     "Thinking level: off",
			SkipAgent: true,
		}, nil
	}
	if !ok {
		return &CommandResult{
			Reply:     fmt.Sprintf("Unknown thinking level: %s. Options: %s", raw, FormatThinkingLevels("", ", ")),
			SkipAgent: true,
			IsError:   true,
		}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("Thinking level set to: %s", level),
		SessionMod: &SessionModification{ThinkLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleFastCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	if raw == "" || raw == "status" {
		mode := "off"
		if ctx.Session != nil && ctx.Session.FastMode {
			mode = "on"
		}
		return &CommandResult{Reply: fmt.Sprintf("Fast mode: %s", mode), SkipAgent: true}, nil
	}
	val, ok := NormalizeFastMode(raw)
	if !ok {
		return &CommandResult{Reply: "Usage: /fast on|off", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("Fast mode: %s", boolToOnOff(val)),
		SessionMod: &SessionModification{FastMode: &val},
		SkipAgent:  true,
	}, nil
}

func handleVerboseCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	level, ok := NormalizeVerboseLevel(raw)
	if !ok && raw == "" {
		current := VerboseOff
		if ctx.Session != nil && ctx.Session.VerboseLevel != "" {
			current = ctx.Session.VerboseLevel
		}
		return &CommandResult{Reply: fmt.Sprintf("Verbose: %s", current), SkipAgent: true}, nil
	}
	if !ok {
		return &CommandResult{Reply: "Usage: /verbose off|on|full", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("Verbose: %s", level),
		SessionMod: &SessionModification{VerboseLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleReasoningCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	level, ok := NormalizeReasoningLevel(raw)
	if !ok {
		return &CommandResult{Reply: "Usage: /reasoning off|on|stream", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("Reasoning: %s", level),
		SessionMod: &SessionModification{ReasoningLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleElevatedCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	level, ok := NormalizeElevatedLevel(raw)
	if !ok {
		return &CommandResult{Reply: "Usage: /elevated off|on|ask|full", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("Elevated: %s", level),
		SessionMod: &SessionModification{ElevatedLevel: level},
		SkipAgent:  true,
	}, nil
}

func handleActivationCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	mode, ok := NormalizeGroupActivation(raw)
	if !ok && raw == "" {
		current := "mention"
		if ctx.Session != nil && ctx.Session.GroupActivation != "" {
			current = string(ctx.Session.GroupActivation)
		}
		return &CommandResult{Reply: fmt.Sprintf("Activation: %s", current), SkipAgent: true}, nil
	}
	if !ok {
		return &CommandResult{Reply: "Usage: /activation mention|always", SkipAgent: true, IsError: true}, nil
	}
	return &CommandResult{
		Reply:      fmt.Sprintf("Activation: %s", mode),
		SessionMod: &SessionModification{GroupActivation: mode},
		SkipAgent:  true,
	}, nil
}

func handleSendPolicyCommand(ctx CommandContext) (*CommandResult, error) {
	raw := ""
	if ctx.Args != nil {
		raw = ctx.Args.Raw
	}
	lowered := strings.ToLower(strings.TrimSpace(raw))
	switch lowered {
	case "on", "off", "inherit":
		return &CommandResult{
			Reply:      fmt.Sprintf("Send policy: %s", lowered),
			SessionMod: &SessionModification{SendPolicy: lowered},
			SkipAgent:  true,
		}, nil
	default:
		return &CommandResult{Reply: "Usage: /send on|off|inherit", SkipAgent: true, IsError: true}, nil
	}
}

func handleUsageCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Token usage display updated.", SkipAgent: true}, nil
}

func handleCompactCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Context compacted.", SkipAgent: true}, nil
}

func handleExportCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Session exported.", SkipAgent: true}, nil
}

func handleContextCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Context report generated.", SkipAgent: true}, nil
}

func handleHelpCommand(ctx CommandContext) (*CommandResult, error) {
	return &CommandResult{Reply: "Use /status to view session info, /new to start fresh, /model to switch models.", SkipAgent: true}, nil
}

func boolToOnOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
