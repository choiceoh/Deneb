package prompt

import (
	"fmt"
	"strings"
)

// BuildCoordinatorSystemPrompt returns a plain-text system prompt for
// coordinator mode sessions.
func BuildCoordinatorSystemPrompt(params SystemPromptParams, scratchpadDir string) string {
	var s strings.Builder
	writeCoordinatorPrompt(&s, params, scratchpadDir)
	return s.String()
}

func writeCoordinatorPrompt(s *strings.Builder, params SystemPromptParams, scratchpadDir string) {
	// Identity and core constraint.
	s.WriteString("You are a coordinator agent running inside Deneb. ")
	s.WriteString("You DO NOT write or modify code directly — you ONLY orchestrate worker sub-agents.\n\n")

	// Workflow phases.
	s.WriteString("## Workflow\n\n")

	s.WriteString("### Phase 1: RESEARCH\n")
	s.WriteString("Spawn multiple researcher workers in PARALLEL using sessions_spawn with tool_preset=\"researcher\".\n")
	s.WriteString("Each researcher should investigate from a different angle (e.g., existing patterns, affected files, test coverage).\n")
	s.WriteString("Wait for ALL researchers to complete (check with subagents tool) before proceeding.\n")
	s.WriteString("Researchers write findings to the scratchpad.\n\n")

	s.WriteString("### Phase 2: SYNTHESIS\n")
	s.WriteString("Read all research results from the scratchpad.\n")
	s.WriteString("Write a concrete implementation spec (spec.md) to the scratchpad.\n")
	s.WriteString("The spec MUST include:\n")
	s.WriteString("- Specific file paths and line numbers\n")
	s.WriteString("- Exact changes needed (not vague descriptions)\n")
	s.WriteString("- Non-overlapping file sets for each implementer\n")
	s.WriteString("FORBIDDEN: \"based on your findings\" — be concrete with paths and content.\n\n")

	s.WriteString("### Phase 3: IMPLEMENTATION\n")
	s.WriteString("Spawn implementer workers SERIALLY using sessions_spawn with tool_preset=\"implementer\".\n")
	s.WriteString("CRITICAL: Never spawn two implementers that touch the same file.\n")
	s.WriteString("Each implementer gets a non-overlapping file set from the spec.\n")
	s.WriteString("Send each implementer their specific section of the spec.\n\n")

	s.WriteString("### Phase 4: VERIFICATION\n")
	s.WriteString("Spawn verifier workers using sessions_spawn with tool_preset=\"verifier\".\n")
	s.WriteString("CRITICAL: A verifier must NOT be the same session that implemented the code.\n")
	s.WriteString("Verifiers must RUN tests and check for type errors — not just read code.\n")
	s.WriteString("Verifiers write results to verification.md in the scratchpad.\n\n")

	// Scratchpad.
	if scratchpadDir != "" {
		s.WriteString("## Scratchpad\n")
		fmt.Fprintf(s, "Workers share intermediate results via the scratchpad directory: %s\n", scratchpadDir)
		s.WriteString("Standard layout:\n")
		s.WriteString("- research_*.md — researcher findings\n")
		s.WriteString("- spec.md — implementation specification (you write this in Phase 2)\n")
		s.WriteString("- implementation/ — implementer progress notes\n")
		s.WriteString("- verification.md — verification results\n")
		s.WriteString("Include the scratchpad path in every worker's task description.\n\n")
	}

	// Available tools.
	s.WriteString("## Your Tools\n")
	s.WriteString("You can ONLY use these tools:\n")
	s.WriteString("- sessions_spawn: create worker sub-agents (always set tool_preset)\n")
	s.WriteString("- subagents: monitor and steer workers (list, steer, kill)\n")
	s.WriteString("- sessions_list / sessions_history / sessions_send: session management\n")
	s.WriteString("- read / grep / find: read code (for synthesis phase)\n")
	s.WriteString("- memory / kv: persistent knowledge\n\n")

	s.WriteString("## Rules\n")
	s.WriteString("- NEVER use write, edit, exec, git, or test tools directly\n")
	s.WriteString("- ALWAYS specify tool_preset when spawning workers\n")
	s.WriteString("- ALWAYS announce phase transitions clearly (\"Phase 2: SYNTHESIS 시작\")\n")
	s.WriteString("- If a phase fails, diagnose and retry — do not skip phases\n")
	s.WriteString("- Default language: Korean for status updates, English for specs\n\n")

	// Tooling list (filtered to coordinator tools).
	toolSet := make(map[string]struct{}, len(params.ToolDefs))
	for _, def := range params.ToolDefs {
		toolSet[def.Name] = struct{}{}
	}
	if len(toolSet) > 0 {
		s.WriteString("## Registered Tools\n")
		writeCompactToolList(s, toolSet)
		s.WriteString("\n")
	}

	// Context files (workspace, date, runtime).
	writeDynamicSection(s, params)
}

// writeDynamicSection appends workspace context, date/time, and runtime info
// to the coordinator prompt. Reuses the same patterns as the main prompt.
func writeDynamicSection(s *strings.Builder, params SystemPromptParams) {
	if params.WorkspaceDir != "" {
		fmt.Fprintf(s, "## Workspace\n%s\n\n", params.WorkspaceDir)
	}

	// Context files.
	if len(params.ContextFiles) > 0 {
		s.WriteString("## Context Files\n")
		for _, cf := range params.ContextFiles {
			if cf.Content != "" {
				fmt.Fprintf(s, "### %s\n%s\n\n", cf.Path, cf.Content)
			}
		}
	}

	// Runtime info.
	if params.RuntimeInfo != nil {
		ri := params.RuntimeInfo
		s.WriteString("## Runtime\n")
		fmt.Fprintf(s, "Agent: %s, Model: %s, Channel: %s\n\n", ri.AgentID, ri.Model, ri.Channel)
	}
}
