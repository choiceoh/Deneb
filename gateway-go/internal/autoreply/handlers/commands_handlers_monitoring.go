// commands_handlers_monitoring.go — Monitoring command handlers.
package handlers

import "fmt"

func handleZeroCallsCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Deps == nil || ctx.Deps.ZeroCallsFn == nil {
		return &CommandResult{Reply: "zero-calls report not available.", SkipAgent: true}, nil
	}

	report := ctx.Deps.ZeroCallsFn()
	if report == nil || len(report.ZeroCalls) == 0 {
		return &CommandResult{
			Reply:     fmt.Sprintf("All %d RPC methods have been called.", report.TotalMethods),
			SkipAgent: true,
		}, nil
	}

	// Build a compact list.
	msg := fmt.Sprintf("**Zero-call RPC methods** (%d / %d)\n\n", len(report.ZeroCalls), report.TotalMethods)
	for _, m := range report.ZeroCalls {
		msg += "• `" + m + "`\n"
	}

	return &CommandResult{Reply: msg, SkipAgent: true}, nil
}
