// executor_context.go — run-scoped cancellation contexts for the agent loop.
package agent

import (
	"context"
	"errors"
	"time"
)

// agentRunContext carries parent values but deliberately hides parent deadlines
// from LLM requests. The LLM client grants a fresh minimum request window when
// it sees a short deadline; agent-level and caller-level timeouts must instead
// arrive as hard cancellation so streaming HTTP requests stop at the run cap.
type agentRunContext struct {
	values context.Context
	cancel context.Context
}

func (c *agentRunContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *agentRunContext) Done() <-chan struct{}       { return c.cancel.Done() }

func (c *agentRunContext) Err() error {
	if err := c.cancel.Err(); err != nil {
		return err
	}
	if err := c.values.Err(); err != nil {
		return err
	}
	return nil
}

func (c *agentRunContext) Value(key any) any {
	if v := c.cancel.Value(key); v != nil {
		return v
	}
	return c.values.Value(key)
}

func newAgentRunContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	cancelCtx, cancelCause := context.WithCancelCause(context.Background())
	runCtx := &agentRunContext{values: parent, cancel: cancelCtx}
	parentStop := context.AfterFunc(parent, func() {
		cancelCause(contextCauseOrCanceled(parent))
	})
	timer := time.AfterFunc(timeout, func() {
		cancelCause(context.DeadlineExceeded)
	})
	return runCtx, func() {
		timer.Stop()
		parentStop()
		cancelCause(context.Canceled)
	}
}

func contextCauseOrCanceled(ctx context.Context) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return context.Canceled
}

// toolExecutionContext preserves the operator-facing timeout semantics for
// tools. The run context uses context.Canceled to force LLM HTTP cancellation,
// but tools that return ctx.Err() should still report deadline exceeded when
// the run ended because its time budget expired.
type toolExecutionContext struct {
	context.Context
}

func (c toolExecutionContext) Err() error {
	err := c.Context.Err()
	if err == nil {
		return nil
	}
	if errors.Is(context.Cause(c.Context), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}

func contextForToolExecution(ctx context.Context) context.Context {
	return toolExecutionContext{Context: ctx}
}
