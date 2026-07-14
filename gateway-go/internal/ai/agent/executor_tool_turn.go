// executor_tool_turn.go — execution and commit-ready state for one tool turn.
package agent

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

const interruptedBeforeToolStart = "Tool not run: agent execution was interrupted before this call started."

// toolTurnOutcome contains everything RunAgent needs to commit a tool-bearing
// assistant turn exactly once. results is always ID-complete when canceled:
// real results are preserved and calls that never started receive a synthetic
// error result, keeping persisted tool_use/tool_result pairs balanced.
type toolTurnOutcome struct {
	results          []llm.ContentBlock
	activities       []ToolActivity
	interruptedNames []string
	editThrashNudges []string
	canceled         bool
}

// executeToolTurn owns the sequential/parallel dispatch choice and reduces the
// per-call execution state into a commit-ready outcome. It deliberately does
// not append budget warnings or edit-thrash nudges: RunAgent does that only for
// a live turn that will continue to another LLM request.
func executeToolTurn(
	ctx context.Context,
	cfg AgentConfig,
	calls []llm.ContentBlock,
	tools ToolExecutor,
	hooks StreamHooks,
	turnReason string,
	turn int,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) toolTurnOutcome {
	if logger == nil {
		logger = slog.Default()
	}

	run := &toolTurnRun{
		cfg:        cfg,
		calls:      calls,
		tools:      tools,
		hooks:      hooks,
		turnReason: turnReason,
		turn:       turn,
		logger:     logger,
		runLog:     runLog,
		outcome: toolTurnOutcome{
			results: make([]llm.ContentBlock, len(calls)),
		},
		lifecycles: make([]toolCallLifecycle, len(calls)),
	}

	if ctx.Err() == nil {
		run.dispatchSegments(ctx)
	}

	run.outcome.canceled = ctx.Err() != nil
	if run.outcome.canceled {
		run.fillInterruptedResults()
	}
	run.collectActivities()

	return run.outcome
}

// toolTurnRun carries one tool turn's fixed inputs plus the outcome and
// per-call lifecycle state being reduced, so the dispatch helpers below share
// state without threading nine parameters through each call.
type toolTurnRun struct {
	cfg        AgentConfig
	calls      []llm.ContentBlock
	tools      ToolExecutor
	hooks      StreamHooks
	turnReason string
	turn       int
	logger     *slog.Logger
	runLog     *agentlog.RunLogger
	outcome    toolTurnOutcome
	lifecycles []toolCallLifecycle
}

// dispatchSegments executes the turn's calls by parallel-safety segments
// (segmentToolCalls): consecutive read-only $ref-free calls overlap; every
// other call is a barrier that runs alone at its emitted position, so
// cross-tool side effects stay exactly as predictable as a fully sequential run.
func (r *toolTurnRun) dispatchSegments(ctx context.Context) {
	provenanceRoot := toolProvenanceRoot(r.tools)
	for _, seg := range segmentToolCalls(r.cfg, r.calls) {
		if ctx.Err() != nil {
			break
		}
		if seg.parallel {
			// Read-only segment: overlap executions. RecordFileMutation is
			// skipped — the parallel-safe set cannot mutate files, so there
			// is no thrash to count.
			parallel := executeToolsParallelTracked(
				ctx,
				r.calls[seg.start:seg.end],
				r.tools,
				r.hooks,
				r.turnReason,
				r.turn,
				r.logger,
				r.runLog,
				r.cfg.ToolLoopDetector,
			)
			copy(r.outcome.results[seg.start:seg.end], parallel.results)
			copy(r.lifecycles[seg.start:seg.end], parallel.calls)
			continue
		}
		r.runSequentialSegment(ctx, seg, provenanceRoot)
	}
}

// runSequentialSegment executes one barrier segment's calls in order, stopping
// at cancellation, and stages edit-thrash nudges for successful mutations.
func (r *toolTurnRun) runSequentialSegment(ctx context.Context, seg toolCallSegment, provenanceRoot string) {
	for i := seg.start; i < seg.end; i++ {
		if ctx.Err() != nil {
			break
		}
		tc := r.calls[i]
		execution := executeOneToolTracked(
			ctx,
			tc,
			r.tools,
			r.hooks,
			r.turnReason,
			r.turn,
			r.logger,
			r.runLog,
			r.cfg.ToolLoopDetector,
		)
		r.outcome.results[i] = execution.block
		r.lifecycles[i].resolved = true
		r.lifecycles[i].interrupted = execution.interrupted

		// Count only successful mutations. The nudge is staged here but is
		// omitted by RunAgent when cancellation means no next LLM turn exists.
		if r.cfg.ToolLoopDetector != nil && !r.outcome.results[i].IsError {
			if nudge := r.cfg.ToolLoopDetector.RecordFileMutation(provenanceRoot, tc.Name, tc.Input); nudge != "" {
				r.outcome.editThrashNudges = append(r.outcome.editThrashNudges, nudge)
			}
		}
	}
}

// fillInterruptedResults gives every unresolved call a synthetic error result
// after cancellation, keeping persisted tool_use/tool_result pairs balanced.
func (r *toolTurnRun) fillInterruptedResults() {
	for i, tc := range r.calls {
		if r.lifecycles[i].resolved {
			continue
		}
		r.outcome.results[i] = llm.ContentBlock{
			Type:      "tool_result",
			ToolUseID: tc.ID,
			Content:   interruptedBeforeToolStart,
			IsError:   true,
		}
		// prepareToolCall already emitted start/emit hooks. If cancellation
		// then prevented dispatch, close that visible lifecycle now. Calls
		// never prepared emitted no start and need no result hook.
		if r.lifecycles[i].prepared && !r.lifecycles[i].dispatched && r.hooks.OnToolResult != nil {
			r.hooks.OnToolResult(tc.Name, tc.ID, interruptedBeforeToolStart, true)
		}
	}
}

// collectActivities records a ToolActivity for every resolved call and, on a
// canceled turn, the names of calls that were interrupted or never resolved.
func (r *toolTurnRun) collectActivities() {
	for i, tc := range r.calls {
		if r.lifecycles[i].resolved {
			block := r.outcome.results[i]
			r.outcome.activities = append(r.outcome.activities, ToolActivity{
				Name:        tc.Name,
				IsError:     block.IsError,
				Turn:        r.turn + 1,
				OutputRunes: len([]rune(block.Content)),
			})
		}
		if r.outcome.canceled && (!r.lifecycles[i].resolved || r.lifecycles[i].interrupted) {
			r.outcome.interruptedNames = append(r.outcome.interruptedNames, tc.Name)
		}
	}
}
