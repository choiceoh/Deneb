// executor_runner.go — staged orchestration for one agent run.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// agentRunner owns the mutable execution state shared by preparation,
// streaming, accounting, recovery, tool dispatch, and grace handling.
type agentRunner struct {
	cfg       AgentConfig
	client    LLMStreamer
	tools     ToolExecutor
	hooks     StreamHooks
	logger    *slog.Logger
	runLog    *agentlog.RunLogger
	runCtx    context.Context
	cancel    context.CancelFunc
	state     *agentRunState
	result    *AgentResult
	journal   *runMessageJournal
	preparer  *turnRequestPreparer
	baseLimit int

	softDeadlineAt time.Time

	softDeadlineFinal bool

	maxToolCallAttempts  int
	toolCallAttemptsUsed int
	streamBytesUsed      int

	// turnRetries collects the CURRENT turn's provider failures (429s, transport
	// errors) so logTurnDetail can stamp them onto turn.llm. Replaced per turn by
	// streamTurn; a turn runs on one goroutine, and the collector is itself
	// mutex-guarded for the streaming watchdog.
	turnRetries *llm.RetryCollector
}

type preparedAgentTurn struct {
	index                int
	request              preparedTurnRequest
	remainingStreamBytes int
}

type completedAgentTurn struct {
	prepared      preparedAgentTurn
	result        *turnResult
	stats         agentTurnStats
	message       *stagedRunMessage
	finishAttempt bool
}

func newAgentRunner(
	ctx context.Context,
	cfg AgentConfig,
	messages []llm.Message,
	client LLMStreamer,
	tools ToolExecutor,
	hooks StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*agentRunner, error) {
	cfg = normalizeAgentConfig(cfg)
	if logger == nil {
		logger = slog.Default()
	}
	maxToolCallAttempts := -1
	if cfg.MaxToolCallAttempts != nil {
		maxToolCallAttempts = *cfg.MaxToolCallAttempts
		if maxToolCallAttempts < 0 {
			return nil, errors.New("agent max tool-call attempt limit must be non-negative")
		}
	}
	runCtx, cancel := newAgentRunContext(ctx, cfg.Timeout)
	state := newAgentRunState(messages, cfg.OnMessagePersist)
	softDeadlineAt := cfg.SoftDeadlineAt
	if softDeadlineAt.IsZero() && cfg.SoftDeadline > 0 {
		softDeadlineAt = time.Now().Add(cfg.SoftDeadline)
	}
	runner := &agentRunner{
		cfg:                 cfg,
		client:              client,
		tools:               tools,
		hooks:               hooks,
		logger:              logger,
		runLog:              runLog,
		runCtx:              runCtx,
		cancel:              cancel,
		state:               state,
		result:              state.result,
		journal:             state.journal,
		baseLimit:           cfg.MaxTokens,
		softDeadlineAt:      softDeadlineAt,
		maxToolCallAttempts: maxToolCallAttempts,
	}
	runner.preparer = newTurnRequestPreparer(&runner.cfg)
	return runner, nil
}

func normalizeAgentConfig(cfg AgentConfig) AgentConfig {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 25
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Minute
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8192
	}
	return cfg
}

func (r *agentRunner) close() {
	r.state.finalize()
	r.cancel()
}

func (r *agentRunner) run() (*AgentResult, error) {
	for turn := 0; r.shouldRunTurn(turn); turn++ {
		done, err := r.runTurn(turn)
		if err != nil {
			return nil, err
		}
		if done {
			return r.result, nil
		}
	}
	r.finishTurnBudget()
	return r.result, nil
}

func (r *agentRunner) shouldRunTurn(turn int) bool {
	return turn < r.cfg.MaxTurns || (!r.cfg.DisableBudgetGrace && r.result.BudgetGraceCall)
}

func (r *agentRunner) runTurn(turn int) (bool, error) {
	prepared, done, err := r.prepareTurn(turn)
	if done || err != nil {
		return done, err
	}
	llmStart := time.Now()
	turnResult, done, err := r.streamTurn(prepared)
	r.result.LLMMs += time.Since(llmStart).Milliseconds()
	if done || err != nil {
		return done, err
	}
	stats, err := r.validateAndAccountTurn(prepared, turnResult)
	if err != nil {
		return false, err
	}
	r.recordTurnTelemetry(prepared, turnResult, stats)

	turnState := &completedAgentTurn{
		prepared:      prepared,
		result:        turnResult,
		stats:         stats,
		message:       r.journal.stage(llm.NewBlockMessage("assistant", turnResult.contentBlocks)),
		finishAttempt: turnResult.stopReason == "end_turn" || len(turnResult.toolCalls) == 0,
	}
	if r.recoverMaxTokens(turnState) {
		return false, nil
	}
	r.persistFinishAttempt(turnState)
	if r.holdForFinalizeGate(turnState) {
		return false, nil
	}
	if turnState.finishAttempt {
		r.finishTurn(turnState)
		return true, nil
	}
	toolStart := time.Now()
	done = r.executeAndCommitToolTurn(turnState)
	r.result.ToolMs += time.Since(toolStart).Milliseconds()
	return done, nil
}

func (r *agentRunner) prepareTurn(turn int) (preparedAgentTurn, bool, error) {
	r.maybeEnterSoftDeadlineFinalMode()
	requestMaxTokens, done := r.requestTokenBudget()
	if done {
		return preparedAgentTurn{}, true, nil
	}
	remainingStreamBytes, err := r.remainingStreamBudget()
	if err != nil {
		return preparedAgentTurn{}, false, err
	}
	r.result.Turns = turn + 1
	prepared := r.preparer.prepare(r.runCtx, turn, r.journal.messages, r.result.ToolActivities)
	if r.softDeadlineFinal {
		// A soft deadline is a final-answer preference, not cancellation. Remove
		// tools and explicitly disable tool choice so this and any defensive retry
		// spend the remaining hard-budget headroom writing the answer.
		prepared.request.Tools = nil
		prepared.request.ToolChoice = llm.FlexibleFromValue("none")
	}
	prepared.request.MaxTokens = requestMaxTokens
	return preparedAgentTurn{
		index:                turn,
		request:              prepared,
		remainingStreamBytes: remainingStreamBytes,
	}, false, nil
}

const softDeadlineWrapUpPrompt = "[System: 응답 시간의 소프트 한도에 도달했습니다. 지금까지 확인한 내용만으로 최종 답변을 작성하세요. 새 도구를 호출하지 말고, 불확실한 부분은 명시하세요.]"

func (r *agentRunner) maybeEnterSoftDeadlineFinalMode() {
	if r.softDeadlineFinal || r.softDeadlineAt.IsZero() || time.Now().Before(r.softDeadlineAt) {
		return
	}
	r.softDeadlineFinal = true
	r.journal.appendEphemeral(llm.NewTextMessage("user", softDeadlineWrapUpPrompt))
	if r.cfg.OnSoftDeadline != nil {
		r.cfg.OnSoftDeadline()
	}
	r.logger.Warn("agent soft deadline reached; forcing no-tools wrap-up",
		"softDeadline", r.cfg.SoftDeadline,
		"turn", r.result.Turns+1)
}

func (r *agentRunner) requestTokenBudget() (int, bool) {
	requestMaxTokens := r.cfg.MaxTokens
	if r.cfg.MaxTotalOutputTokens <= 0 {
		return requestMaxTokens, false
	}
	remaining := r.cfg.MaxTotalOutputTokens - r.result.Usage.OutputTokens
	if remaining <= 0 {
		r.result.StopReason = "max_total_tokens"
		return 0, true
	}
	if requestMaxTokens > remaining {
		requestMaxTokens = remaining
	}
	return requestMaxTokens, false
}

func (r *agentRunner) remainingStreamBudget() (int, error) {
	remaining := r.cfg.MaxStreamBytes
	if remaining <= 0 {
		return remaining, nil
	}
	remaining -= r.streamBytesUsed
	if remaining <= 0 {
		return 0, ErrStreamLimit
	}
	return remaining, nil
}

func (r *agentRunner) streamTurn(prepared preparedAgentTurn) (*turnResult, bool, error) {
	// Fresh per turn: a collector reused across turns would report the run's
	// total on every row and make a single bad turn look like a whole bad run.
	turnCtx, collector := llm.WithRetryCollector(prepared.request.ctx)
	r.turnRetries = collector
	outcome, err := runStreamingTurnWithPolicy(
		turnCtx,
		r.client,
		prepared.request.request,
		r.hooks,
		r.cfg.StreamIdleTimeout,
		r.logger,
		prepared.index,
		r.cfg.DisableStreamRetry,
		prepared.remainingStreamBytes,
	)
	r.streamBytesUsed += outcome.streamBytes
	r.result.Stream.record(outcome)
	if err == nil {
		return outcome.result, false, nil
	}
	if outcome.preOutputIdle() {
		r.result.StopReason = "timeout"
		return nil, true, nil
	}
	if prepared.request.ctx.Err() != nil {
		r.result.Stream.TerminationReason = string(streamTerminationContextDone)
		r.result.StopReason = stopReasonFromCtx(prepared.request.ctx)
		return nil, true, nil //nolint:nilerr // context cancellation is recorded as a controlled stop, not a run failure
	}
	if outcome.initialConnectionFailed() {
		return nil, false, fmt.Errorf("stream chat (turn %d): %w", prepared.index, err)
	}
	return nil, false, fmt.Errorf("consume stream (turn %d): %w", prepared.index, err)
}

func (r *agentRunner) validateAndAccountTurn(prepared preparedAgentTurn, result *turnResult) (agentTurnStats, error) {
	if err := r.validateProviderModel(prepared.index, result); err != nil {
		return agentTurnStats{}, err
	}
	if err := r.validateStopShape(prepared.index, result); err != nil {
		return agentTurnStats{}, err
	}
	if err := r.chargeOutputTokens(result); err != nil {
		return agentTurnStats{}, err
	}
	if err := r.reserveToolCallAttempts(prepared.index, len(result.toolCalls)); err != nil {
		return agentTurnStats{}, err
	}
	return r.state.recordTurn(result), nil
}

func (r *agentRunner) validateProviderModel(turn int, result *turnResult) error {
	if r.cfg.RequireProviderModel {
		if strings.TrimSpace(result.providerModel) == "" {
			return fmt.Errorf("provider did not report a model identifier on turn %d", turn)
		}
		if r.result.ProviderModel != "" && r.result.ProviderModel != result.providerModel {
			return fmt.Errorf("provider model changed from %q to %q", r.result.ProviderModel, result.providerModel)
		}
	}
	if result.providerModel != "" {
		r.result.ProviderModel = result.providerModel
	}
	return nil
}

func (r *agentRunner) validateStopShape(turn int, result *turnResult) error {
	if r.cfg.RequireStrictStopShape {
		expected := "end_turn"
		if len(result.toolCalls) > 0 {
			expected = "tool_use"
		}
		if result.stopReason != expected {
			return fmt.Errorf(
				"%w: turn %d reported %q with %d tool calls; expected %q",
				ErrInvalidStopShape, turn+1, result.stopReason, len(result.toolCalls), expected,
			)
		}
	}
	if r.cfg.RequireExplicitStopReason && result.stopReason == "" {
		return errors.New("provider did not report an explicit stop reason")
	}
	return nil
}

func (r *agentRunner) chargeOutputTokens(result *turnResult) error {
	charged := result.usage.OutputTokens
	if r.cfg.MaxTotalOutputTokens > 0 {
		charged = generatedOutputTokenCharge(result.contentBlocks, charged)
		remaining := r.cfg.MaxTotalOutputTokens - r.result.Usage.OutputTokens
		if charged > remaining {
			return errors.New("provider exceeded the configured total output-token budget")
		}
	}
	result.usage.OutputTokens = charged
	return nil
}

func (r *agentRunner) reserveToolCallAttempts(turn, requested int) error {
	if r.maxToolCallAttempts < 0 {
		return nil
	}
	remaining := r.maxToolCallAttempts - r.toolCallAttemptsUsed
	if requested > remaining {
		return fmt.Errorf(
			"%w: turn %d requested %d calls with %d remaining",
			ErrToolCallLimit, turn+1, requested, remaining,
		)
	}
	r.toolCallAttemptsUsed += requested
	return nil
}

func (r *agentRunner) recordTurnTelemetry(prepared preparedAgentTurn, result *turnResult, stats agentTurnStats) {
	r.logTurnComplete(prepared.index, result, stats)
	r.recordTokenFeedback(prepared.request.request, result)
	r.logTurnDetail(prepared, result)
	if r.cfg.OnTurn != nil {
		r.cfg.OnTurn(prepared.index+1, r.result.Usage.InputTokens+r.result.Usage.OutputTokens)
	}
}

func (r *agentRunner) logTurnComplete(turn int, result *turnResult, stats agentTurnStats) {
	r.logger.Info("agent turn complete",
		"turn", turn,
		"turnInputTokens", result.usage.InputTokens,
		"turnOutputTokens", result.usage.OutputTokens,
		"turnCacheReadTokens", result.usage.CacheReadInputTokens,
		"turnCacheCreationTokens", result.usage.CacheCreationInputTokens,
		"accInputTokens", r.result.Usage.InputTokens,
		"messages", len(r.journal.messages),
		"textChars", stats.textChars,
		"textHead", stats.textHead,
		"toolCount", stats.toolCount,
		"toolNames", strings.Join(stats.toolNames, ","),
		"toolInputBytes", stats.toolInputBytes,
		"stopReason", result.stopReason)
}

func (r *agentRunner) recordTokenFeedback(request llm.ChatRequest, result *turnResult) {
	if result.usage.InputTokens <= 0 || r.cfg.DisableTokenFeedback {
		return
	}
	estimator := tokenest.ForModel(r.cfg.Model)
	estimated := estimator.CountBytes(request.System.Bytes())
	for _, message := range request.Messages {
		estimated += estimator.CountBytes(message.Content.Bytes())
	}
	tokenest.RecordFeedback(estimator.Family(), estimated, result.usage.InputTokens)
}

func (r *agentRunner) logTurnDetail(prepared preparedAgentTurn, result *turnResult) {
	if r.runLog == nil {
		return
	}
	thinking := prepared.request.thinking
	r.runLog.LogTurnLLM(agentlog.TurnLLMData{
		Turn:                prepared.index + 1,
		InputTokens:         result.usage.InputTokens,
		OutputTokens:        result.usage.OutputTokens,
		StopReason:          result.stopReason,
		TextLen:             len(result.text),
		ToolCalls:           len(result.toolCalls),
		CacheReadTokens:     result.usage.CacheReadInputTokens,
		CacheCreationTokens: result.usage.CacheCreationInputTokens,
		ThinkingOff:         thinking != nil && thinking.Type == "disabled",
		ObsRunes:            r.state.priorToolOutputRunes,
		Retries:             r.turnRetries.Count(),
		RetryMix:            r.turnRetries.Summary(),
		RateLimited:         r.turnRetries.RateLimited(),
		ProviderModel:       result.providerModel,
	})
}

func (r *agentRunner) recoverMaxTokens(turn *completedAgentTurn) bool {
	if turn.result.stopReason != "max_tokens" || len(turn.result.toolCalls) != 0 ||
		r.cfg.MaxOutputTokensRecovery <= 0 || r.state.maxTokenRecoveries >= r.cfg.MaxOutputTokensRecovery {
		return false
	}
	attempt := r.state.noteRecovery()
	if r.isThinkingRunaway(turn) {
		r.recoverThinkingRunaway(turn, attempt)
		return true
	}
	r.recoverTruncatedAnswer(turn, attempt)
	return true
}

func (r *agentRunner) isThinkingRunaway(turn *completedAgentTurn) bool {
	return r.cfg.ThinkingOffRetry != nil && strings.TrimSpace(turn.result.text) == "" && turn.stats.thinkingText != ""
}

func (r *agentRunner) recoverThinkingRunaway(turn *completedAgentTurn, attempt int) {
	r.preparer.overrideThinkingOnce(r.cfg.ThinkingOffRetry)
	r.cfg.MaxTokens = r.baseLimit
	r.logger.Info("max_tokens recovery: thinking runaway — retrying with thinking off",
		"attempt", attempt, "maxAttempts", r.cfg.MaxOutputTokensRecovery)
	turn.message.append()
	r.journal.append(llm.NewTextMessage("user",
		"[직전 응답이 분석(생각)만 하다 토큰 한도에 걸렸습니다. 추가 분석 없이 곧바로 최종 답변만 작성하세요.]"))
}

func (r *agentRunner) recoverTruncatedAnswer(turn *completedAgentTurn, attempt int) {
	scale := 2.0
	if index := attempt - 1; index < len(r.cfg.MaxOutputTokensScaleFactors) {
		scale = r.cfg.MaxOutputTokensScaleFactors[index]
	}
	r.cfg.MaxTokens = int(float64(r.baseLimit) * scale)
	r.logger.Info("max_tokens recovery: scaling output tokens and injecting resume",
		"attempt", attempt,
		"maxAttempts", r.cfg.MaxOutputTokensRecovery,
		"baseMaxTokens", r.baseLimit,
		"newMaxTokens", r.cfg.MaxTokens)
	turn.message.append()
	r.journal.append(llm.NewTextMessage("user",
		"[Output was truncated due to token limit. Resume directly from where you left off — no apology, no recap.]"))
}

func (r *agentRunner) persistFinishAttempt(turn *completedAgentTurn) {
	if turn.finishAttempt && turn.result.text != "" {
		turn.message.persist()
	}
}

func (r *agentRunner) holdForFinalizeGate(turn *completedAgentTurn) bool {
	if !turn.finishAttempt || r.cfg.FinalizeGate == nil {
		return false
	}
	if r.result.BudgetGraceCall {
		_ = r.cfg.FinalizeGate(-1)
		return false
	}
	gatePrompt := r.cfg.FinalizeGate(turn.prepared.index)
	if gatePrompt == "" {
		return false
	}
	r.logger.Info("finalize gate: holding finish for verification", "turn", turn.prepared.index)
	turn.message.append()
	r.journal.append(llm.NewTextMessage("user", gatePrompt))
	return true
}

func (r *agentRunner) finishTurn(turn *completedAgentTurn) {
	if turn.result.text != "" {
		turn.message.persist()
	}
	if r.result.BudgetGraceCall {
		r.result.StopReason = StopReasonMaxTurnsGraceful
		r.result.BudgetGraceCall = false
		return
	}
	r.result.StopReason = turn.result.stopReason
	if r.result.StopReason == "" {
		r.result.StopReason = "end_turn"
	}
}

func (r *agentRunner) executeAndCommitToolTurn(turn *completedAgentTurn) bool {
	if turn.prepared.index == 0 && r.cfg.StripImagesAfterFirstTurn {
		r.journal.messages = stripBase64ImagesFromHistory(r.journal.messages)
	}
	currentTurnStart := len(r.journal.messages)
	turn.message.append()
	toolCtx := contextForToolExecution(turn.prepared.request.ctx)
	outcome := executeToolTurn(
		toolCtx,
		r.cfg,
		turn.result.toolCalls,
		r.tools,
		r.hooks,
		extractThinkingText(turn.result.contentBlocks),
		turn.prepared.index,
		r.logger,
		r.runLog,
	)
	r.state.recordToolActivities(outcome.activities)
	if r.cfg.OnToolTurn != nil {
		r.cfg.OnToolTurn(turn.prepared.index+1, outcome.activities)
	}
	toolResults := r.toolResultsForNextTurn(turn.prepared.index, outcome)
	r.journal.append(llm.NewBlockMessage("user", toolResults))
	if r.finishCanceledToolTurn(toolCtx, outcome) {
		return true
	}
	r.compactPriorToolResults(turn.prepared.index, currentTurnStart)
	r.advanceGraceBudget(turn.prepared.index)
	return false
}

func (r *agentRunner) toolResultsForNextTurn(turn int, outcome toolTurnOutcome) []llm.ContentBlock {
	toolResults := outcome.results
	if outcome.canceled {
		return toolResults
	}
	for _, nudge := range outcome.editThrashNudges {
		toolResults = append(toolResults, llm.ContentBlock{Type: "text", Text: nudge})
	}
	// Late-arriving mid-run notifications (subagent completions) ride the new
	// tool-results user message as trailing text blocks — never the System
	// prompt, whose mid-run rewrite would cold-start content-prefix provider
	// caches (see AgentConfig.DeferredTurnNotices).
	if r.cfg.DeferredTurnNotices != nil {
		for _, notice := range r.cfg.DeferredTurnNotices() {
			if notice != "" {
				toolResults = append(toolResults, llm.ContentBlock{Type: "text", Text: notice})
			}
		}
	}
	return r.preparer.appendBudgetWarning(turn, toolResults)
}

func (r *agentRunner) finishCanceledToolTurn(ctx context.Context, outcome toolTurnOutcome) bool {
	if !outcome.canceled {
		return false
	}
	r.result.StopReason = stopReasonFromCtx(ctx)
	r.result.InterruptedToolNames = append(r.result.InterruptedToolNames, outcome.interruptedNames...)
	return true
}

func (r *agentRunner) compactPriorToolResults(turn, currentTurnStart int) {
	if r.cfg.DisablePriorToolResultCompaction {
		return
	}
	compacted := CompactPriorToolResults(r.journal.messages, currentTurnStart)
	if compacted == 0 {
		return
	}
	r.logger.Info("compacted prior tool results",
		"turn", turn,
		"blocksCompacted", compacted)
}

func (r *agentRunner) advanceGraceBudget(turn int) {
	if r.result.BudgetGraceCall {
		r.result.BudgetGraceCall = false
		return
	}
	if r.cfg.DisableBudgetGrace || turn+1 < r.cfg.MaxTurns || r.result.BudgetExhaustedInjected {
		return
	}
	r.journal.append(llm.NewTextMessage("user", GraceCallPrompt))
	r.result.BudgetExhaustedInjected = true
	r.result.BudgetGraceCall = true
	r.logger.Warn("agent turn budget exhausted; issuing grace wrap-up call",
		"maxTurns", r.cfg.MaxTurns, "turn", turn)
}

func (r *agentRunner) finishTurnBudget() {
	if r.cfg.FinalizeGate != nil {
		_ = r.cfg.FinalizeGate(-1)
	}
	if r.result.BudgetExhaustedInjected {
		r.result.StopReason = StopReasonMaxTurnsGraceful
		return
	}
	r.result.StopReason = "max_turns"
}
