package handlers

// HandleSubagentsHelpAction returns the subagent help text.
func HandleSubagentsHelpAction() *SubagentCommandResult {
	return subagentStopWithText(BuildSubagentsHelp())
}
