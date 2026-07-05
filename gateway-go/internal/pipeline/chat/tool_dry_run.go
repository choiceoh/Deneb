// tool_dry_run.go — side-effect suppression for eval/replay runs.
//
// When a run's context carries toolctx.WithToolDryRun, ToolRegistry.Execute
// dispatches only tools on the read-only allowlist below; every other tool
// returns a stub without its fn being invoked. This lets harnesses (behavioral
// skill replay, prompt-regression turns, puppet rehearsals) drive the REAL
// agent loop against the REAL registry — schemas, preset filtering, caching,
// audit counters all live — without sending messages, writing files, spawning
// processes, or mutating any external system.
//
// The polarity is default-deny: a tool must be proven pure to execute in
// dry-run, so newly added tools are automatically suppressed until someone
// consciously allowlists them. Multiplexed tools whose mutating/read-only
// split lives in an "action" argument (wiki, calendar, todo, notebook …) stay
// suppressed entirely — argument-level classification is a follow-up, and
// stubbing a read is only a fidelity cost, never a safety one.
package chat

// dryRunSafeTools are tools whose execution has no side effects on the
// workspace, external systems, or run-visible state beyond their own result.
// (fetch_tools activates deferred schemas, but that is run-scoped, in-memory
// and exactly what a replay harness needs to exercise.)
var dryRunSafeTools = map[string]struct{}{
	"read":           {},
	"grep":           {},
	"fetch_tools":    {},
	"read_spillover": {},
}

// dryRunStub is the result returned in place of executing a suppressed tool.
// It tells the model plainly what happened so a replayed transcript stays
// coherent instead of looking like a transport failure.
func dryRunStub(name string) string {
	return "[dry-run] tool \"" + name + "\" was not executed — side effects are suppressed in this run. " +
		"Assume the call would have succeeded and continue."
}
