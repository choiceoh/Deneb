// executor_tool_turn.go — execution and commit-ready state for one tool turn.
package agent

import (
	"context"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
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

	outcome := toolTurnOutcome{
		results: make([]llm.ContentBlock, len(calls)),
	}
	lifecycles := make([]toolCallLifecycle, len(calls))

	if ctx.Err() == nil && parallelSafeTurn(cfg, calls) {
		parallel := executeToolsParallelTracked(
			ctx,
			calls,
			tools,
			hooks,
			turnReason,
			turn,
			logger,
			runLog,
			cfg.ToolLoopDetector,
		)
		outcome.results = parallel.results
		copy(lifecycles, parallel.calls)
	} else if ctx.Err() == nil {
		provenanceRoot := toolProvenanceRoot(tools)
		for i, tc := range calls {
			if ctx.Err() != nil {
				break
			}

			execution := executeOneToolTracked(
				ctx,
				tc,
				tools,
				hooks,
				turnReason,
				turn,
				logger,
				runLog,
				cfg.ToolLoopDetector,
			)
			outcome.results[i] = execution.block
			lifecycles[i].resolved = true
			lifecycles[i].interrupted = execution.interrupted

			// Count only successful mutations. The nudge is staged here but is
			// omitted by RunAgent when cancellation means no next LLM turn exists.
			if cfg.ToolLoopDetector != nil && !outcome.results[i].IsError {
				if nudge := cfg.ToolLoopDetector.RecordFileMutation(provenanceRoot, tc.Name, tc.Input); nudge != "" {
					outcome.editThrashNudges = append(outcome.editThrashNudges, nudge)
				}
			}
		}
	}

	outcome.canceled = ctx.Err() != nil
	if outcome.canceled {
		for i, tc := range calls {
			if lifecycles[i].resolved {
				continue
			}
			outcome.results[i] = llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: tc.ID,
				Content:   interruptedBeforeToolStart,
				IsError:   true,
			}
			// prepareToolCall already emitted start/emit hooks. If cancellation
			// then prevented dispatch, close that visible lifecycle now. Calls
			// never prepared emitted no start and need no result hook.
			if lifecycles[i].prepared && !lifecycles[i].dispatched && hooks.OnToolResult != nil {
				hooks.OnToolResult(tc.Name, tc.ID, interruptedBeforeToolStart, true)
			}
		}
	}

	for i, tc := range calls {
		if lifecycles[i].resolved {
			block := outcome.results[i]
			outcome.activities = append(outcome.activities, ToolActivity{
				Name:        tc.Name,
				IsError:     block.IsError,
				Turn:        turn + 1,
				OutputRunes: len([]rune(block.Content)),
			})
		}
		if outcome.canceled && (!lifecycles[i].resolved || lifecycles[i].interrupted) {
			outcome.interruptedNames = append(outcome.interruptedNames, tc.Name)
		}
	}

	return outcome
}
