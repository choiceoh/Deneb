// run_context_prewarm.go — workspace + context-file pre-warming for a chat run
// (split out of run_exec.go per docs/research/improvement-ideas.md §1.1).
package chat

// prewarmPromptWorkspace resolves the workspace dir and pre-warms the context
// file snapshot for this session so disk I/O happens before the parallel prep
// phase (no-op if already cached from a prior turn).
func prewarmPromptWorkspace(params RunParams, deps runDeps) string {
	workspaceDir := params.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = resolveWorkspaceDirForPrompt()
	}
	if !deps.briefcaseMode {
		loadFactAwareContextFiles(workspaceDir, params.SessionKey)
	}
	return workspaceDir
}
