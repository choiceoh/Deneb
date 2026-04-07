package prompt

import (
	"fmt"
	"strings"
)

// WorkerPromptAddition returns a system prompt addition for worker sessions
// that have a tool preset. This gives the worker role-specific instructions
// and scratchpad context. Returns empty string for unknown/empty presets.
func WorkerPromptAddition(preset, scratchpadDir string) string {
	var s strings.Builder
	switch preset {
	case "researcher":
		writeResearcherPrompt(&s, scratchpadDir)
	case "implementer":
		writeImplementerPrompt(&s, scratchpadDir)
	case "verifier":
		writeVerifierPrompt(&s, scratchpadDir)
	default:
		return ""
	}
	return s.String()
}

func writeResearcherPrompt(s *strings.Builder, scratchpadDir string) {
	s.WriteString("\n## Worker Role: Researcher\n")
	s.WriteString("You are a research worker spawned by a coordinator agent.\n")
	s.WriteString("Your job is to investigate the codebase and report findings.\n\n")
	s.WriteString("### Instructions\n")
	s.WriteString("- Use read, grep, find, and other exploration tools to investigate thoroughly.\n")
	s.WriteString("- You CANNOT write, edit, or execute code — only read and analyze.\n")
	s.WriteString("- Be specific: include file paths, line numbers, and code snippets in your findings.\n")
	s.WriteString("- Focus on answering the specific question in your task description.\n")
	if scratchpadDir != "" {
		fmt.Fprintf(s, "- Write your findings to a file in: %s (e.g., research_<topic>.md)\n", scratchpadDir)
	}
	s.WriteString("- When done, provide a clear summary of your findings.\n")
}

func writeImplementerPrompt(s *strings.Builder, scratchpadDir string) {
	s.WriteString("\n## Worker Role: Implementer\n")
	s.WriteString("You are an implementation worker spawned by a coordinator agent.\n")
	s.WriteString("Your job is to make specific code changes as described in your task.\n\n")
	s.WriteString("### Instructions\n")
	s.WriteString("- Follow the implementation spec exactly — do not deviate or add extras.\n")
	s.WriteString("- Only modify files assigned to you — do not touch files outside your scope.\n")
	s.WriteString("- After making changes, run relevant tests to verify your work.\n")
	s.WriteString("- If tests fail, fix the issues before reporting completion.\n")
	if scratchpadDir != "" {
		fmt.Fprintf(s, "- Write progress notes to: %s/implementation/\n", scratchpadDir)
	}
	s.WriteString("- When done, summarize what you changed and test results.\n")
}

func writeVerifierPrompt(s *strings.Builder, scratchpadDir string) {
	s.WriteString("\n## Worker Role: Verifier\n")
	s.WriteString("You are a verification worker spawned by a coordinator agent.\n")
	s.WriteString("Your job is to verify that recent code changes are correct.\n\n")
	s.WriteString("### Instructions\n")
	s.WriteString("- You MUST run tests — do not just read code and guess.\n")
	s.WriteString("- Check for type errors, lint issues, and test failures.\n")
	s.WriteString("- Verify the changes match the implementation spec.\n")
	s.WriteString("- Report any issues with specific file paths and descriptions.\n")
	if scratchpadDir != "" {
		fmt.Fprintf(s, "- Write verification results to: %s/verification.md\n", scratchpadDir)
	}
	s.WriteString("- When done, provide a clear pass/fail summary with details.\n")
}
