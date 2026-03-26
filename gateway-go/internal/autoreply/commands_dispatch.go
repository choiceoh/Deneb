package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
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
// Supports two handler tiers:
//  1. CommandHandlerFull pipeline — typed handlers with full params (TS commands-core.ts pattern)
//  2. CommandRouter bridge — delegates to the existing CommandRouter/CommandHandler system
//
// Mirrors src/auto-reply/reply/commands-core.ts handleCommands().
type CommandDispatcher struct {
	handlers       []CommandHandlerFull
	router         *CommandRouter   // bridge to existing handler system
	registry       *CommandRegistry // command normalization/detection
	acpRegistry    *ACPRegistry     // sub-agent lifecycle tracking
	sendPolicyFunc SendPolicyFunc
	depsFactory    func() *CommandDeps // builds deps for command handlers
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

// SetRouter connects the existing CommandRouter so builtin commands
// (model, think, status, etc.) are reachable through the full dispatch pipeline.
func (d *CommandDispatcher) SetRouter(router *CommandRouter) {
	d.router = router
}

// SetRegistry connects the CommandRegistry for normalization and detection.
func (d *CommandDispatcher) SetRegistry(registry *CommandRegistry) {
	d.registry = registry
}

// SetACPRegistry connects the ACP registry for sub-agent operations.
func (d *CommandDispatcher) SetACPRegistry(acpRegistry *ACPRegistry) {
	d.acpRegistry = acpRegistry
}

// SetSendPolicyFunc configures the send policy resolver.
func (d *CommandDispatcher) SetSendPolicyFunc(fn SendPolicyFunc) {
	d.sendPolicyFunc = fn
}

// SetDepsFactory configures the factory that builds CommandDeps for handlers.
func (d *CommandDispatcher) SetDepsFactory(fn func() *CommandDeps) {
	d.depsFactory = fn
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

	// Run through typed full-params handlers first.
	for _, handler := range d.handlers {
		result := handler(params, true)
		if result != nil {
			return *result
		}
	}

	// Bridge to existing CommandRouter if available.
	// This connects the 37+ builtin command handlers (model, think, status, etc.)
	// through the full dispatch pipeline.
	if d.router != nil {
		cmdKey := extractDispatchCommandKey(params.Command.CommandBodyNormalized)
		if cmdKey != "" && d.router.HasHandler(cmdKey) {
			deps := &CommandDeps{}
			if d.depsFactory != nil {
				deps = d.depsFactory()
			}
			routerCtx := CommandContext{
				Command:    cmdKey,
				Args:       extractDispatchCommandArgs(params.Command.CommandBodyNormalized, cmdKey),
				Body:       params.Ctx.Body,
				SessionKey: params.SessionKey,
				Channel:    params.Command.Channel,
				IsGroup:    params.IsGroup,
				Msg:        params.Ctx,
				Session: &types.SessionState{
					SessionKey:     params.SessionKey,
					AgentID:        params.AgentID,
					Channel:        params.Command.Channel,
					IsGroup:        params.IsGroup,
					Model:          params.Model,
					Provider:       params.Provider,
					ThinkLevel:     params.ResolvedThinkLevel,
					VerboseLevel:   params.ResolvedVerboseLevel,
					ReasoningLevel: params.ResolvedReasoningLevel,
					ElevatedLevel:  params.ResolvedElevatedLevel,
				},
				Deps: deps,
			}

			routerResult, err := d.router.Dispatch(routerCtx)
			if err == nil && routerResult != nil {
				var reply *types.ReplyPayload
				if routerResult.Reply != "" {
					reply = &types.ReplyPayload{
						Text:    routerResult.Reply,
						IsError: routerResult.IsError,
					}
				} else if len(routerResult.Payloads) > 0 {
					reply = &routerResult.Payloads[0]
				}
				return CommandHandlerFullResult{
					Reply:          reply,
					ShouldContinue: !routerResult.SkipAgent,
				}
			}
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

// extractDispatchCommandKey extracts the command key from a normalized body.
func extractDispatchCommandKey(body string) string {
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, "/") {
		return ""
	}
	end := strings.IndexAny(trimmed[1:], " \t\n")
	if end == -1 {
		return trimmed[1:]
	}
	return trimmed[1 : end+1]
}

// extractDispatchCommandArgs extracts arguments after the command key.
func extractDispatchCommandArgs(body, cmdKey string) *CommandArgs {
	prefix := "/" + cmdKey
	if len(body) <= len(prefix) {
		return nil
	}
	rest := body[len(prefix):]
	if len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
		return &CommandArgs{Raw: strings.TrimSpace(rest)}
	}
	return nil
}

// applyResetTailContext rewrites context fields with the tail text after a reset command.
func applyResetTailContext(ctx *types.MsgContext, resetTail string) {
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
