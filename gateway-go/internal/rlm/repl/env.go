package repl

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"
)

// maxOutputLen is the maximum number of bytes captured from print() output
// before truncation. Prevents runaway output from consuming memory.
const maxOutputLen = 20_000

// Env is a Starlark REPL environment that persists state across executions.
// Variables assigned in one Execute() call are available in the next.
type Env struct {
	thread   *starlark.Thread
	globals  starlark.StringDict
	printBuf *bytes.Buffer
	printMu  sync.Mutex
	finalVal *string
	timeout  time.Duration
}

// EnvConfig configures a new REPL environment.
type EnvConfig struct {
	Messages     []MessageEntry
	LLMQueryFn   LLMQueryFunc
	LLMBatchFn   LLMBatchFunc
	RLMQueryFn   RLMQueryFunc
	Timeout      time.Duration // per-execution timeout (default: 30s)
}

// NewEnv creates a new Starlark REPL environment with conversation context
// and LLM callback functions injected as globals.
func NewEnv(ctx context.Context, cfg EnvConfig) *Env {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	env := &Env{
		printBuf: &bytes.Buffer{},
		timeout:  cfg.Timeout,
	}

	env.globals = starlark.StringDict{
		// Context: conversation history as a list of structs
		"context": messagesToStarlark(cfg.Messages),

		// LLM callbacks
		"llm_query": builtinLLMQuery(ctx, cfg.LLMQueryFn),
		"FINAL":     builtinFinal(&env.finalVal),
		"FINAL_VAR": builtinFinalVar(env),
		"SHOW_VARS": builtinShowVars(env),

		// Regex utilities
		"regex_search":  builtinRegexSearch(),
		"regex_findall": builtinRegexFindall(),

		// Override print to capture output
		"print": builtinPrint(env.printBuf, &env.printMu),

		// Common Python builtins that Starlark provides
		"True":  starlark.True,
		"False": starlark.False,
		"None":  starlark.None,
	}

	// Optional: batch LLM
	if cfg.LLMBatchFn != nil {
		env.globals["llm_query_batch"] = builtinLLMBatch(ctx, cfg.LLMBatchFn)
	}

	// Optional: recursive RLM
	if cfg.RLMQueryFn != nil {
		env.globals["rlm_query"] = builtinRLMQuery(ctx, cfg.RLMQueryFn, env)
	}

	// Thread with cancellation support
	env.thread = &starlark.Thread{
		Name: "rlm-repl",
		Print: func(_ *starlark.Thread, msg string) {
			env.printMu.Lock()
			defer env.printMu.Unlock()
			if env.printBuf.Len() < maxOutputLen {
				env.printBuf.WriteString(msg)
				env.printBuf.WriteByte('\n')
			}
		},
	}

	return env
}

// ExecuteResult holds the output of a single REPL execution.
type ExecuteResult struct {
	Stdout      string  // captured print() output
	FinalAnswer *string // non-nil if FINAL() or FINAL_VAR() was called
	Error       string  // non-empty if execution failed
}

// Execute runs Starlark code in the REPL environment.
// Variables persist across calls. The code is preprocessed to handle
// Python f-strings and import statements.
func (e *Env) Execute(code string) ExecuteResult {
	// Reset per-execution state
	e.printMu.Lock()
	e.printBuf.Reset()
	e.printMu.Unlock()
	e.finalVal = nil

	// Preprocess: f-strings → %, import → removed
	code = Preprocess(code)

	// Execute with timeout via goroutine + channel
	type execResult struct {
		err error
	}
	ch := make(chan execResult, 1)

	// Set cancel function on thread for timeout
	done := make(chan struct{})
	e.thread.SetLocal("cancel", done)

	go func() {
		newGlobals, err := starlark.ExecFileOptions(
			&syntax.FileOptions{
				Set:             true, // allow set()
				GlobalReassign:  true, // allow reassigning globals
				TopLevelControl: true, // allow if/for at top level
			},
			e.thread,
			"repl.star",
			code,
			e.globals,
		)
		// Merge new/updated variables back into globals for persistence.
		for k, v := range newGlobals {
			e.globals[k] = v
		}
		ch <- execResult{err: err}
	}()

	// Wait for completion or timeout
	timer := time.NewTimer(e.timeout)
	defer timer.Stop()

	select {
	case result := <-ch:
		if result.err != nil {
			return ExecuteResult{
				Stdout: e.captureOutput(),
				Error:  formatStarlarkError(result.err),
			}
		}
	case <-timer.C:
		e.thread.Cancel("execution timeout")
		// Wait for goroutine to finish after cancellation
		<-ch
		return ExecuteResult{
			Stdout: e.captureOutput(),
			Error:  fmt.Sprintf("execution timed out after %v", e.timeout),
		}
	}

	return ExecuteResult{
		Stdout:      e.captureOutput(),
		FinalAnswer: e.finalVal,
	}
}

// captureOutput returns the accumulated print output, truncating if needed.
func (e *Env) captureOutput() string {
	e.printMu.Lock()
	defer e.printMu.Unlock()

	s := e.printBuf.String()
	if len(s) > maxOutputLen {
		half := maxOutputLen / 2
		s = s[:half] + fmt.Sprintf("\n\n... (truncated %d chars) ...\n\n", len(s)-maxOutputLen) + s[len(s)-half:]
	}
	return s
}

// formatStarlarkError formats a Starlark error for display to the LLM.
func formatStarlarkError(err error) string {
	if evalErr, ok := err.(*starlark.EvalError); ok {
		return evalErr.Backtrace()
	}
	return err.Error()
}

// ResetFinal clears any FINAL() value so the loop can check per-iteration.
func (e *Env) ResetFinal() {
	e.finalVal = nil
}

// HasFinal returns true if a final answer has been set.
func (e *Env) HasFinal() bool {
	return e.finalVal != nil
}

// FinalAnswer returns the final answer if set, or empty string.
func (e *Env) FinalAnswer() string {
	if e.finalVal != nil {
		return *e.finalVal
	}
	return ""
}

// ── Context integration ─────────────────────────────────────────────────────

type envContextKey struct{}

// WithEnv stores a REPL environment in the context for the current request.
func WithEnv(ctx context.Context, env *Env) context.Context {
	return context.WithValue(ctx, envContextKey{}, env)
}

// FromContext retrieves the per-request REPL environment.
func FromContext(ctx context.Context) *Env {
	env, _ := ctx.Value(envContextKey{}).(*Env)
	return env
}

// starlarkstruct is imported for structToMessage in builtins.go
var _ = starlarkstruct.Default
