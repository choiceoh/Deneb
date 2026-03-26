package autoreply

import (
	"log/slog"
	"regexp"
	"strings"
)

// ResetCommandAction identifies the type of session reset.
type ResetCommandAction string

const (
	ResetActionNew   ResetCommandAction = "new"
	ResetActionReset ResetCommandAction = "reset"
)

var resetCommandRe = regexp.MustCompile(`^/(new|reset)(?:\s|$)`)

// SendPolicyFunc resolves the send policy for a session.
// Returns "deny" to block the message, any other value to allow.
type SendPolicyFunc func(sessionKey, channel, chatType string) string

// CommandDispatcher manages the full command dispatch pipeline.
// It handles /new, /reset detection, hook emission, and routes to
// registered command handlers.
//
// Mirrors src/auto-reply/reply/commands-core.ts handleCommands().
type CommandDispatcher struct {
	handlers       []CommandHandlerFull
	sendPolicyFunc SendPolicyFunc
	logger         *slog.Logger
}

// NewCommandDispatcher creates a new dispatcher with the given handlers.
// Handlers are tried in order; the first to return a non-nil result wins.
func NewCommandDispatcher(handlers []CommandHandlerFull, logger *slog.Logger) *CommandDispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &CommandDispatcher{
		handlers: handlers,
		logger:   logger,
	}
}

// SetSendPolicyFunc configures the send policy resolver.
func (d *CommandDispatcher) SetSendPolicyFunc(fn SendPolicyFunc) {
	d.sendPolicyFunc = fn
}

// DispatchCommands runs the full command dispatch pipeline.
//
// 1. Detects /new or /reset commands
// 2. Blocks unauthorized reset attempts
// 3. Emits reset hooks for authorized resets
// 4. Iterates through handlers in priority order
// 5. Defaults to shouldContinue=true if no handler matches
func (d *CommandDispatcher) DispatchCommands(params HandleCommandsFullParams) CommandHandlerFullResult {
	// Detect reset/new commands.
	match := resetCommandRe.FindStringSubmatch(params.Command.CommandBodyNormalized)
	resetRequested := match != nil

	if resetRequested && !params.Command.IsAuthorizedSender {
		d.logger.Debug("ignoring /reset from unauthorized sender",
			"senderId", params.Command.SenderID,
		)
		return CommandHandlerFullResult{ShouldContinue: false}
	}

	// Emit reset hooks for authorized reset/new commands.
	if resetRequested && params.Command.IsAuthorizedSender {
		action := ResetActionNew
		if match[1] == "reset" {
			action = ResetActionReset
		}

		// Extract tail text after /new or /reset.
		resetTail := ""
		if match != nil {
			resetTail = strings.TrimSpace(
				params.Command.CommandBodyNormalized[len(match[0]):],
			)
		}

		d.logger.Info("reset command detected",
			"action", action,
			"sessionKey", params.SessionKey,
			"hasTail", resetTail != "",
		)

		// If there's tail text after /new, rewrite context and continue dispatch.
		if resetTail != "" {
			applyResetTailContext(params.Ctx, resetTail)
			if params.RootCtx != nil && params.RootCtx != params.Ctx {
				applyResetTailContext(params.RootCtx, resetTail)
			}
			return CommandHandlerFullResult{ShouldContinue: false}
		}
	}

	// Run through command handlers.
	for _, handler := range d.handlers {
		result := handler(params, true)
		if result != nil {
			return *result
		}
	}

	// No handler matched — check send policy before continuing to agent.
	if d.sendPolicyFunc != nil {
		policy := d.sendPolicyFunc(params.SessionKey, params.Command.Channel, "")
		if policy == "deny" {
			d.logger.Debug("send blocked by policy",
				"sessionKey", params.SessionKey,
			)
			return CommandHandlerFullResult{ShouldContinue: false}
		}
	}

	return CommandHandlerFullResult{ShouldContinue: true}
}

// applyResetTailContext rewrites context fields with the tail text after a reset command.
func applyResetTailContext(ctx *MsgContext, resetTail string) {
	ctx.Body = resetTail
	ctx.RawBody = resetTail
	ctx.CommandBody = resetTail
	ctx.BodyForCommands = resetTail
	ctx.BodyForAgent = resetTail
}

// IsResetCommand returns true if the text starts with /new or /reset.
func IsResetCommand(text string) bool {
	return resetCommandRe.MatchString(strings.TrimSpace(text))
}

// ParseResetCommand extracts the action and tail from a /new or /reset command.
// Returns empty action if the text is not a reset command.
func ParseResetCommand(text string) (action ResetCommandAction, tail string) {
	trimmed := strings.TrimSpace(text)
	match := resetCommandRe.FindStringSubmatch(trimmed)
	if match == nil {
		return "", ""
	}
	if match[1] == "reset" {
		action = ResetActionReset
	} else {
		action = ResetActionNew
	}
	tail = strings.TrimSpace(trimmed[len(match[0]):])
	return
}
