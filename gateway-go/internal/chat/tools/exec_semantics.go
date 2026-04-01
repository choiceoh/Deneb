// exec_semantics.go provides command-specific exit code interpretation.
//
// Many Unix utilities use non-zero exit codes for non-error outcomes.
// A blanket "exit 0 = success, else = error" misclassifies these and
// causes the LLM to treat normal outputs (e.g., "grep found no matches")
// as failures. This module teaches the agent the correct semantics.
//
// Inspired by Claude Code's commandSemantics.ts pattern.
package tools

import (
	"strings"
)

// commandSemantic describes how to interpret a command's exit code.
// isError returns true if the given exit code represents a real error
// for this particular command.
type commandSemantic struct {
	// isError returns true when the exit code is a genuine error.
	isError func(exitCode int) bool
	// hint provides a short explanation appended to output when the
	// command exits with a non-zero code that is NOT an error.
	hint func(exitCode int) string
}

// commandSemantics maps base command names to their semantic interpreters.
// Only commands where exit code 1 is commonly misinterpreted are listed.
var commandSemantics = map[string]commandSemantic{
	// grep/rg: exit 1 = no matches found (not an error), 2+ = error
	"grep": {
		isError: func(code int) bool { return code >= 2 },
		hint:    func(int) string { return "(no matches found)" },
	},
	"rg": {
		isError: func(code int) bool { return code >= 2 },
		hint:    func(int) string { return "(no matches found)" },
	},
	"egrep": {
		isError: func(code int) bool { return code >= 2 },
		hint:    func(int) string { return "(no matches found)" },
	},
	"fgrep": {
		isError: func(code int) bool { return code >= 2 },
		hint:    func(int) string { return "(no matches found)" },
	},
	// diff: exit 1 = differences found, 2+ = error
	"diff": {
		isError: func(code int) bool { return code >= 2 },
		hint:    func(int) string { return "(differences found)" },
	},
	// test/[: exit 1 = condition is false (not an error), 2+ = error
	"test": {
		isError: func(code int) bool { return code >= 2 },
		hint:    func(int) string { return "(condition evaluated to false)" },
	},
	"[": {
		isError: func(code int) bool { return code >= 2 },
		hint:    func(int) string { return "(condition evaluated to false)" },
	},
	// find: exit 1 = some files/dirs inaccessible (partial success)
	"find": {
		isError: func(code int) bool { return code >= 2 },
		hint:    func(int) string { return "(partial: some paths inaccessible)" },
	},
}

// InterpretExitCode checks whether a non-zero exit code is actually an error
// for the given command string. Returns:
//   - isError: true if the exit code represents a genuine error
//   - hint: optional explanation for non-error exits (empty if isError)
func InterpretExitCode(command string, exitCode int) (isError bool, hint string) {
	if exitCode == 0 {
		return false, ""
	}

	base := extractBaseCommand(command)
	sem, ok := commandSemantics[base]
	if !ok {
		// Default: any non-zero exit is an error.
		return true, ""
	}

	if sem.isError(exitCode) {
		return true, ""
	}
	return false, sem.hint(exitCode)
}

// extractBaseCommand extracts the base command name from a potentially
// complex command string. For pipelines, the last command determines
// the exit code, so we extract that.
func extractBaseCommand(command string) string {
	// For pipelines, the exit code comes from the last command.
	if idx := strings.LastIndex(command, "|"); idx >= 0 {
		command = strings.TrimSpace(command[idx+1:])
	}

	// Strip leading env vars (FOO=bar cmd ...) and sudo.
	parts := strings.Fields(command)
	for len(parts) > 0 {
		p := parts[0]
		if strings.Contains(p, "=") {
			parts = parts[1:]
			continue
		}
		if p == "sudo" || p == "env" || p == "nice" || p == "nohup" || p == "time" {
			parts = parts[1:]
			continue
		}
		break
	}
	if len(parts) == 0 {
		return ""
	}

	// Extract basename (e.g., /usr/bin/grep → grep).
	name := parts[0]
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}
