// dispatch_config.go — Full dispatch orchestration from config.
// Mirrors src/auto-reply/reply/dispatch-from-config.ts (664 LOC),
// dispatch-acp.ts (367 LOC), dispatch-acp-delivery.ts (189 LOC),
// followup-runner.ts (415 LOC), origin-routing.ts (29 LOC),
// dispatcher-registry.ts (58 LOC), provider-dispatcher.ts (44 LOC).
package autoreply

import (
	"context"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/dispatch"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/session"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// DispatchConfig holds the resolved configuration for message dispatch.
type DispatchConfig struct {
	SessionKey   string
	AgentID      string
	Channel      string
	To           string
	AccountID    string
	ThreadID     string
	Model        string
	Provider     string
	IsGroup      bool
	IsHeartbeat  bool
	ReplyOptions types.GetReplyOptions
}

// DispatchResult holds the outcome of a full dispatch cycle.
type DispatchResult struct {
	Payloads []types.ReplyPayload
	Handled  bool
	Error    error
}

// DispatchFromConfig runs the full reply dispatch pipeline from config.
func DispatchFromConfig(ctx context.Context, msg *types.MsgContext, cfg DispatchConfig, deps ReplyDeps) DispatchResult {
	// 1. Check for abort trigger.
	if session.IsAbortRequestText(msg.Body) {
		// Record abort in memory so subsequent messages within the cooldown
		// window are not re-dispatched to the agent.
		if deps.AbortMemory != nil {
			deps.AbortMemory.Record(cfg.SessionKey, time.Now().UnixMilli())
		}
		session.EmitSessionHook(session.SessionHookEvent{
			Type:       "abort",
			SessionKey: cfg.SessionKey,
			Reason:     "user abort trigger",
			Timestamp:  time.Now().UnixMilli(),
		})
		return DispatchResult{Handled: true}
	}

	// 1b. Skip messages to recently aborted sessions (3-second cooldown).
	if deps.AbortMemory != nil && deps.AbortMemory.WasRecentlyAborted(cfg.SessionKey, 3000) {
		return DispatchResult{Handled: true}
	}

	// 1c. Skip messages that fall before the abort cutoff marker.
	if deps.AbortCutoff != nil {
		cutoff := session.ReadAbortCutoffFromSessionEntry(deps.AbortCutoff)
		if cutoff != nil {
			if session.ShouldSkipMessageByAbortCutoff(cutoff.MessageSid, cutoff.Timestamp, msg.MessageSid, nil) {
				return DispatchResult{Handled: true}
			}
		}
	}

	// 2. Check for command.
	if deps.Registry != nil && deps.Router != nil {
		normalized := deps.Registry.NormalizeCommandBody(msg.Body, "")
		if deps.Registry.HasControlCommand(normalized, "") {
			cmdKey := extractCommandKey(normalized)
			// Use ParseCommandArgs for proper positional argument parsing
			// when the command definition has typed args.
			args := extractCommandArgs(normalized, cmdKey)
			if args != nil {
				if cmd := deps.Registry.FindCommandByNativeName(cmdKey); cmd != nil {
					parsed := handlers.ParseCommandArgs(cmd, args.Raw)
					if parsed != nil {
						args = parsed
					}
				}
			}
			result, err := deps.Router.Dispatch(handlers.CommandContext{
				Command:    cmdKey,
				Args:       args,
				Body:       msg.Body,
				SessionKey: cfg.SessionKey,
				Channel:    cfg.Channel,
				IsGroup:    cfg.IsGroup,
				Msg:        msg,
				Deps:       deps.CommandDeps,
			})
			if err == nil && result != nil && result.SkipAgent {
				var payloads []types.ReplyPayload
				if result.Reply != "" {
					payloads = append(payloads, types.ReplyPayload{Text: result.Reply, IsError: result.IsError})
				}
				payloads = append(payloads, result.Payloads...)
				return DispatchResult{Payloads: payloads, Handled: true}
			}
		}
	}

	// 2b. Check for inline command tokens (e.g., "!model gpt-4" embedded in text).
	// These indicate the message body contains inline commands that should be
	// processed as directives during agent reply generation.
	if handlers.HasInlineCommandTokens(msg.Body) {
		msg.CommandSource = "inline"
	}

	// 3. Generate reply via agent.
	payloads, err := GetReplyFromConfig(ctx, msg, cfg.ReplyOptions, deps)
	if err != nil {
		return DispatchResult{Error: err}
	}

	return DispatchResult{Payloads: payloads, Handled: true}
}

func extractCommandArgs(normalized, cmdKey string) *handlers.CommandArgs {
	prefix := "/" + cmdKey
	if len(normalized) <= len(prefix) {
		return nil
	}
	rest := normalized[len(prefix):]
	if len(rest) > 0 && (rest[0] == ' ' || rest[0] == '\t') {
		raw := rest[1:]
		return &handlers.CommandArgs{Raw: raw}
	}
	return nil
}

// OriginRouting determines the reply target based on message origin.
type OriginRouting struct {
	Channel   string
	To        string
	AccountID string
	ThreadID  string
}

// ResolveOriginRouting extracts routing info from the inbound message.
func ResolveOriginRouting(msg *types.MsgContext) OriginRouting {
	return OriginRouting{
		Channel:   msg.Channel,
		To:        msg.To,
		AccountID: msg.AccountID,
		ThreadID:  msg.ThreadID,
	}
}

// DispatcherRegistry tracks active dispatchers for session coordination.
type DispatcherRegistry struct {
	mu          sync.Mutex
	dispatchers map[string]*dispatch.ReplyDispatcher
}

// NewDispatcherRegistry creates a new dispatcher registry.
func NewDispatcherRegistry() *DispatcherRegistry {
	return &DispatcherRegistry{
		dispatchers: make(map[string]*dispatch.ReplyDispatcher),
	}
}

// Register adds a dispatcher for a session.
func (r *DispatcherRegistry) Register(sessionKey string, d *dispatch.ReplyDispatcher) {
	r.mu.Lock()
	r.dispatchers[sessionKey] = d
	r.mu.Unlock()
}

// Unregister removes a dispatcher.
func (r *DispatcherRegistry) Unregister(sessionKey string) {
	r.mu.Lock()
	delete(r.dispatchers, sessionKey)
	r.mu.Unlock()
}

// Get returns the active dispatcher for a session.
func (r *DispatcherRegistry) Get(sessionKey string) *dispatch.ReplyDispatcher {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dispatchers[sessionKey]
}

// FollowupRunner handles multi-turn follow-up executions.
type FollowupRunner struct {
	agent    AgentExecutor
	maxTurns int
}

// NewFollowupRunner creates a followup runner with a turn limit.
func NewFollowupRunner(agent AgentExecutor, maxTurns int) *FollowupRunner {
	if maxTurns <= 0 {
		maxTurns = 10
	}
	return &FollowupRunner{agent: agent, maxTurns: maxTurns}
}

// RunFollowups executes follow-up turns until the agent signals completion.
func (f *FollowupRunner) RunFollowups(ctx context.Context, initial AgentTurnConfig, firstResult *AgentTurnResult) ([]types.ReplyPayload, error) {
	allPayloads := make([]types.ReplyPayload, 0)
	allPayloads = append(allPayloads, firstResult.Payloads...)

	for turn := 1; turn < f.maxTurns; turn++ {
		// Check if the agent signaled it needs another turn (e.g., tool_use).
		if !needsFollowup(firstResult) {
			break
		}

		result, err := f.agent.RunTurn(ctx, initial)
		if err != nil {
			return allPayloads, err
		}
		allPayloads = append(allPayloads, result.Payloads...)
		firstResult = result
	}

	return allPayloads, nil
}

func needsFollowup(result *AgentTurnResult) bool {
	if result == nil {
		return false
	}
	// Check if any payload has tool use content.
	for _, p := range result.Payloads {
		if IsToolUseContent(p.Text) {
			return true
		}
	}
	return false
}

// ACPStreamSettings configures ACP streaming behavior.
type ACPStreamSettings struct {
	Enabled    bool
	BufferSize int
	FlushMs    int64
}

// DefaultACPStreamSettings returns sensible defaults.
func DefaultACPStreamSettings() ACPStreamSettings {
	return ACPStreamSettings{
		Enabled:    true,
		BufferSize: 4096,
		FlushMs:    100,
	}
}

// ACPResetTarget specifies an ACP target for reset operations.
type ACPResetTarget struct {
	AgentID    string
	SessionKey string
}
