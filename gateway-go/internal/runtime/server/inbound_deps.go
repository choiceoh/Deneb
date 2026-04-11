// Package server — InboundProcessor dependency builders and chat executor.
//
// Extracted from inbound.go: CommandDeps assembly, model candidate building,
// subagent command dispatch, RPC zero-calls report, and chatSendExecutor
// (the bridge from autoreply.AgentExecutor to chat.Handler.Send).
package server

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/metrics"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/model"
	subagentpkg "github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/subagent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// buildCommandDeps creates a CommandDeps populated with server-level status data.
// sessionKey is used to look up the current session's last failure reason for /status.
func (p *InboundProcessor) buildCommandDeps(sessionKey string) *handlers.CommandDeps {
	sd := &handlers.StatusDeps{
		Version:   p.server.version,
		StartedAt: p.server.startedAt,
	}
	if p.server.sessions != nil {
		sd.SessionCount = p.server.sessions.Count()
	}

	// Per-provider usage stats.
	if p.server.usageTracker != nil {
		report := p.server.usageTracker.Status()
		if report != nil && len(report.Providers) > 0 {
			sd.ProviderUsage = make(map[string]*handlers.ProviderUsageStats, len(report.Providers))
			for name, ps := range report.Providers {
				sd.ProviderUsage[name] = &handlers.ProviderUsageStats{
					Calls:  ps.Calls,
					Input:  ps.Tokens.Input,
					Output: ps.Tokens.Output,
				}
			}
		}
	}

	// Channel health.
	if p.server.channelHealth != nil {
		snapshot := p.server.channelHealth.HealthSnapshot()
		if len(snapshot) > 0 {
			sd.ChannelHealth = make([]handlers.ChannelHealthEntry, len(snapshot))
			for i, ch := range snapshot {
				sd.ChannelHealth[i] = handlers.ChannelHealthEntry{
					ID:      ch.ChannelID,
					Healthy: ch.Healthy,
					Reason:  ch.Reason,
				}
			}
		}
	}

	// Session-specific failure reason for /status.
	if sessionKey != "" && p.server.sessions != nil {
		if sess := p.server.sessions.Get(sessionKey); sess != nil {
			sd.LastFailureReason = sess.FailureReason
		}
	}

	var subagentRunsFn func() []subagentpkg.SubagentRunRecord
	if p.server.acpDeps != nil && p.server.acpDeps.Registry != nil {
		reg := p.server.acpDeps.Registry
		key := sessionKey
		subagentRunsFn = func() []subagentpkg.SubagentRunRecord {
			agents := reg.List(key)
			runs := make([]subagentpkg.SubagentRunRecord, len(agents))
			for i, a := range agents {
				runs[i] = subagentpkg.SubagentRunRecord{
					RunID:           a.ID,
					ChildSessionKey: a.SessionKey,
					RequesterKey:    key,
					SpawnDepth:      a.Depth,
					WorkspaceDir:    a.WorkspaceDir,
					CreatedAt:       a.SpawnedAt,
					StartedAt:       a.SpawnedAt,
					EndedAt:         a.EndedAt,
					OutcomeStatus:   a.Status,
				}
			}
			return runs
		}
	}

	var zeroCallsFn func() *handlers.RPCZeroCallsReport
	if p.server.dispatcher != nil {
		disp := p.server.dispatcher
		zeroCallsFn = func() *handlers.RPCZeroCallsReport {
			return buildZeroCallsReport(disp)
		}
	}

	return &handlers.CommandDeps{
		Status:              sd,
		SubagentRuns:        subagentRunsFn,
		ZeroCallsFn:         zeroCallsFn,
		MorningLetterDataFn: p.buildMorningLetterDataFn(),
	}
}

// buildMorningLetterDataFn returns a data collection function that includes
// diary logging when wiki is enabled.
func (p *InboundProcessor) buildMorningLetterDataFn() func(ctx context.Context) (string, error) {
	var diaryDir string
	if p.server.wikiStore != nil {
		diaryDir = p.server.wikiStore.DiaryDir()
	}
	if diaryDir == "" {
		return tools.CollectMorningLetterData
	}
	return func(ctx context.Context) (string, error) {
		return tools.CollectMorningLetterDataWithOpts(ctx, tools.MorningLetterOpts{DiaryDir: diaryDir})
	}
}

// buildModelCandidates converts the model role registry into autoreply
// ModelCandidates for directive-based model resolution (/model, !model).
func (p *InboundProcessor) buildModelCandidates() []model.ModelCandidate {
	reg := p.server.modelRegistry
	if reg == nil {
		return nil
	}
	configured := reg.ConfiguredModels()
	seen := make(map[string]struct{})
	var candidates []model.ModelCandidate
	for role, cfg := range configured {
		if cfg.Model == "" {
			continue
		}
		fullID := cfg.ProviderID + "/" + cfg.Model
		if _, ok := seen[fullID]; ok {
			continue
		}
		seen[fullID] = struct{}{}
		candidates = append(candidates, model.ModelCandidate{
			Provider: cfg.ProviderID,
			Model:    cfg.Model,
			Label:    string(role),
		})
	}
	return candidates
}

// dispatchSubagentCommand routes a subagent command through the subagent
// dispatcher, wiring ACP registry deps when available.
func (p *InboundProcessor) dispatchSubagentCommand(
	normalized string,
	sessionKey string,
	channelName string,
	accountID string,
	threadID string,
	senderID string,
	isGroup bool,
) *subagentpkg.SubagentCommandResult {
	var deps *subagentpkg.SubagentCommandDeps
	if p.server.acpDeps != nil && p.server.acpDeps.Registry != nil {
		cfg := subagentpkg.ACPCommandDepsConfig{
			Infra: p.server.acpDeps.Infra,
		}
		// Wire SessionSendFn from the ACP deps when available so that
		// /subagents send, /steer, and spawn initial-message delivery work.
		if p.server.acpDeps.SessionSendFn != nil {
			cfg.SessionSendFn = p.server.acpDeps.SessionSendFn
		}
		// Wire SessionBindings so /focus, /unfocus, and /agents commands
		// can resolve and mutate conversation-to-session bindings.
		if p.server.acpDeps.Bindings != nil {
			cfg.SessionBindings = p.server.acpDeps.Bindings
		}
		// Wire TranscriptLoader so /subagents log can display session history.
		if p.server.acpDeps.TranscriptLoader != nil {
			loader := p.server.acpDeps.TranscriptLoader
			cfg.TranscriptLoader = func(sessionKey string, limit int) ([]subagentpkg.ChatLogMessage, error) {
				roles, contents, err := loader(sessionKey, limit)
				if err != nil {
					return nil, err
				}
				msgs := make([]subagentpkg.ChatLogMessage, len(roles))
				for i := range roles {
					msgs[i] = subagentpkg.ChatLogMessage{Role: roles[i], Content: contents[i]}
				}
				return msgs, nil
			}
		}
		deps = subagentpkg.NewSubagentCommandDepsFromACP(
			p.server.acpDeps.Registry, cfg,
		)
	}
	return subagentpkg.HandleSubagentsCommand(
		normalized, sessionKey, channelName, accountID, threadID,
		senderID, isGroup, true, // isAuthorized: single-user deployment
		deps,
	)
}

// buildZeroCallsReport cross-references registered RPC methods with
// RPCRequestsTotal to find methods that have never been called.
func buildZeroCallsReport(disp *rpc.Dispatcher) *handlers.RPCZeroCallsReport {
	methods := disp.Methods()
	sort.Strings(methods)

	counts := metrics.RPCRequestsTotal.Snapshot()

	var zeroCalls []string
	for _, m := range methods {
		okKey := m + "\x00" + "ok"
		errKey := m + "\x00" + "error"
		if counts[okKey]+counts[errKey] == 0 {
			zeroCalls = append(zeroCalls, m)
		}
	}

	return &handlers.RPCZeroCallsReport{
		ZeroCalls:    zeroCalls,
		TotalMethods: len(methods),
	}
}

// Compile-time interface compliance.
var _ autoreply.AgentExecutor = (*chatSendExecutor)(nil)

// chatSendExecutor bridges the autoreply.AgentExecutor interface to
// chat.Handler.Send. When the autoreply pipeline decides the message should
// go to the agent (not handled by a command or abort), RunTurn builds a
// chat.send request frame and dispatches it through the existing async
// chat handler pipeline.
type chatSendExecutor struct {
	chatHandler *chat.Handler
	chatID      string
	messageID   int64
	attachments []chat.ChatAttachment
	logger      *slog.Logger
	didSend     bool // set to true after chat.send dispatch
}

func (e *chatSendExecutor) RunTurn(ctx context.Context, cfg autoreply.AgentTurnConfig) (*autoreply.AgentTurnResult, error) {
	// Build delivery context with triggering message ID for reply threading.
	delivery := map[string]any{
		"channel": "telegram",
		"to":      e.chatID,
	}
	if e.messageID != 0 {
		delivery["messageId"] = strconv.FormatInt(e.messageID, 10)
	}

	sendParams := map[string]any{
		"sessionKey":  cfg.SessionKey,
		"message":     cfg.Message,
		"delivery":    delivery,
		"clientRunId": shortid.New("run"),
	}
	if cfg.Model != "" {
		sendParams["model"] = cfg.Model
	}
	if len(e.attachments) > 0 {
		sendParams["attachments"] = e.attachments
	}

	req, err := protocol.NewRequestFrame(
		"tg-"+e.chatID+"-"+strconv.FormatInt(e.messageID, 10),
		"chat.send",
		sendParams,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build chat.send request: %w", err)
	}

	sendCtx, sendCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer sendCancel()
	resp := e.chatHandler.Send(sendCtx, req)
	if resp != nil && !resp.OK {
		errMsg := "unknown error"
		if resp.Error != nil {
			errMsg = resp.Error.Message
		}
		e.logger.Warn("chat.send failed via autoreply executor",
			"chatId", e.chatID,
			"error", errMsg,
		)
	}

	e.didSend = true

	// Return empty result — actual reply delivery is async via chat handler.
	return &autoreply.AgentTurnResult{}, nil
}
