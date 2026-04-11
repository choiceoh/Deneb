// commands_handlers_info.go — Status command handler.
package handlers

func handleStatusCommand(ctx CommandContext) (*CommandResult, error) {
	if ctx.Session == nil {
		return &CommandResult{Reply: "No active session.", SkipAgent: true}, nil
	}
	s := ctx.Session
	report := StatusReport{
		SessionKey:     s.SessionKey,
		AgentID:        s.AgentID,
		Model:          s.Model,
		Provider:       s.Provider,
		Channel:        s.Channel,
		IsGroup:        s.IsGroup,
		FastMode:       s.FastMode,
		VerboseLevel:   s.VerboseLevel,
		ReasoningLevel: s.ReasoningLevel,
		ElevatedLevel:  s.ElevatedLevel,
	}
	// Populate server-level fields from StatusDeps.
	if ctx.Deps != nil && ctx.Deps.Status != nil {
		sd := ctx.Deps.Status
		report.Version = sd.Version
		report.StartedAt = sd.StartedAt
		report.SessionCount = sd.SessionCount
		report.ProviderUsage = sd.ProviderUsage
		report.ChannelHealth = sd.ChannelHealth
		report.LastFailureReason = sd.LastFailureReason
	}
	return &CommandResult{Reply: BuildStatusMessage(report), SkipAgent: true}, nil
}
