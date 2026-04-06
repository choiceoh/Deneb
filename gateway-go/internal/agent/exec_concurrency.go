// exec_concurrency.go classifies exec tool commands as read-only or mutating
// for adaptive concurrency decisions. Read-only exec commands (go test, git
// status, ls, etc.) can safely run in parallel with other read-only tools.
package agent

import (
	"encoding/json"
	"strings"
)

// IsReadOnlyExecCommand checks whether an exec tool invocation runs a
// read-only command that is safe for concurrent execution. It parses the
// "command" field from the JSON input and classifies it against known
// read-only binaries and subcommands.
//
// Conservative: returns false for anything it cannot confidently classify.
func IsReadOnlyExecCommand(input json.RawMessage) bool {
	var p struct {
		Command    string `json:"command"`
		Background bool   `json:"background"`
	}
	if json.Unmarshal(input, &p) != nil || p.Command == "" {
		return false
	}
	// Background commands run asynchronously — not safe to classify as
	// concurrent since they outlive the batch.
	if p.Background {
		return false
	}
	return isReadOnlyCommand(p.Command)
}

// isReadOnlyCommand checks whether a shell command string is read-only.
// Handles pipelines and command chains (&&, ||, ;).
func isReadOnlyCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}

	// Output redirection to a file means the command writes state.
	// Allow safe redirections: >/dev/null, 2>&1, 2>/dev/null.
	if hasFileRedirection(cmd) {
		return false
	}

	// Split on chain operators to get individual command segments.
	// Each segment must independently be read-only.
	segments := splitCommandChain(cmd)
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		// Pipeline: split on | and check each stage.
		stages := strings.Split(seg, "|")
		for _, stage := range stages {
			stage = strings.TrimSpace(stage)
			if stage == "" {
				continue
			}
			if !isReadOnlySimpleCommand(stage) {
				return false
			}
		}
	}
	return true
}

// isReadOnlySimpleCommand classifies a single command (no pipes, no chains).
func isReadOnlySimpleCommand(cmd string) bool {
	bin, sub := extractBinaryAndSubcommand(cmd)
	if bin == "" {
		return false
	}

	// Check simple read-only binaries (no subcommand needed).
	if readOnlyBinaries[bin] {
		return true
	}

	// Check compound commands with subcommand verification.
	if subs, ok := readOnlySubcommands[bin]; ok && sub != "" {
		return subs[sub]
	}

	return false
}

// extractBinaryAndSubcommand parses a simple command into its base binary
// and first subcommand. Skips leading env var assignments (KEY=VALUE).
func extractBinaryAndSubcommand(cmd string) (binary, subcommand string) {
	fields := strings.Fields(cmd)
	i := 0
	// Skip env var assignments (e.g., GOOS=linux).
	for i < len(fields) && strings.Contains(fields[i], "=") && !strings.HasPrefix(fields[i], "-") {
		i++
	}
	if i >= len(fields) {
		return "", ""
	}
	binary = fields[i]
	// Strip path prefix: /usr/bin/git -> git.
	if idx := strings.LastIndex(binary, "/"); idx >= 0 {
		binary = binary[idx+1:]
	}
	// Get subcommand (first non-flag argument after binary).
	for j := i + 1; j < len(fields); j++ {
		if !strings.HasPrefix(fields[j], "-") {
			subcommand = fields[j]
			break
		}
	}
	return binary, subcommand
}

// hasFileRedirection detects output redirection to files.
// Returns false for safe redirections like >/dev/null or 2>&1.
func hasFileRedirection(cmd string) bool {
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '>' {
			continue
		}
		// Skip 2>&1 pattern.
		if i > 0 && cmd[i-1] == '&' {
			continue
		}
		// Look at what follows >.
		rest := cmd[i+1:]
		if len(rest) > 0 && rest[0] == '>' {
			rest = rest[1:] // >> append
		}
		rest = strings.TrimLeft(rest, " \t")
		// Safe targets.
		if strings.HasPrefix(rest, "/dev/null") {
			continue
		}
		if strings.HasPrefix(rest, "&1") || strings.HasPrefix(rest, "&2") {
			continue
		}
		return true
	}
	return false
}

// splitCommandChain splits a command on ;, &&, and || boundaries.
func splitCommandChain(cmd string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]

		// Track quoting to avoid splitting inside strings.
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(ch)
			continue
		}
		if inSingle || inDouble {
			current.WriteByte(ch)
			continue
		}

		// Check for chain operators.
		if ch == ';' {
			segments = append(segments, current.String())
			current.Reset()
			continue
		}
		if ch == '&' && i+1 < len(cmd) && cmd[i+1] == '&' {
			segments = append(segments, current.String())
			current.Reset()
			i++ // skip second &
			continue
		}
		if ch == '|' && i+1 < len(cmd) && cmd[i+1] == '|' {
			segments = append(segments, current.String())
			current.Reset()
			i++ // skip second |
			continue
		}

		current.WriteByte(ch)
	}
	if current.Len() > 0 {
		segments = append(segments, current.String())
	}
	return segments
}

// readOnlyBinaries are commands that never modify state regardless of arguments.
var readOnlyBinaries = map[string]bool{
	// File content inspection
	"cat": true, "head": true, "tail": true, "less": true, "more": true,
	"wc": true, "file": true, "stat": true, "readlink": true,
	"md5sum": true, "sha256sum": true, "sha1sum": true,
	"xxd": true, "hexdump": true, "strings": true, "od": true,

	// Search
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true,
	"ack": true, "find": true, "fd": true, "locate": true,
	"which": true, "whereis": true,

	// Listing/tree
	"ls": true, "tree": true, "exa": true, "eza": true,
	"du": true, "df": true,

	// Text processing (stdout-only, no file modification)
	"sort": true, "uniq": true, "cut": true, "tr": true,
	"jq": true, "yq": true, "column": true, "paste": true,
	"fold": true, "nl": true, "rev": true, "base64": true,

	// Diff/compare
	"diff": true, "cmp": true,

	// System info
	"uname": true, "hostname": true, "whoami": true, "id": true,
	"env": true, "printenv": true, "date": true, "uptime": true,
	"nproc": true, "arch": true,

	// Process/network info
	"ps": true, "pgrep": true, "lsof": true, "ss": true,
	"netstat": true, "free": true,

	// Output/test
	"echo": true, "printf": true, "true": true, "false": true, "test": true,

	// Version/help
	"man": true,
}

// readOnlySubcommands maps compound commands to their safe-for-read subcommands.
var readOnlySubcommands = map[string]map[string]bool{
	"git": {
		"status": true, "log": true, "diff": true, "show": true,
		"branch": true, "tag": true, "remote": true, "blame": true,
		"rev-parse": true, "rev-list": true, "describe": true,
		"ls-files": true, "ls-tree": true, "cat-file": true,
		"shortlog": true, "config": true, "version": true,
		"stash": true, // `git stash list` etc.
	},
	"go": {
		"test": true, "vet": true, "build": true, "list": true,
		"version": true, "env": true, "doc": true,
	},
	"cargo": {
		"test": true, "check": true, "clippy": true, "build": true,
		"doc": true, "metadata": true, "version": true, "tree": true,
		"bench": true, "fmt": true,
	},
	"buf": {
		"lint": true, "breaking": true, "build": true, "ls-files": true,
	},
	"npm": {
		"test": true, "run": true, "list": true, "ls": true,
		"outdated": true, "view": true, "version": true,
	},
	"pnpm": {
		"test": true, "run": true, "list": true, "ls": true,
		"outdated": true, "view": true, "why": true,
	},
	"yarn": {
		"test": true, "run": true, "list": true,
		"info": true, "why": true,
	},
	"docker": {
		"ps": true, "images": true, "inspect": true, "logs": true,
		"stats": true, "top": true, "version": true, "info": true,
	},
	"kubectl": {
		"get": true, "describe": true, "logs": true,
		"top": true, "version": true, "cluster-info": true,
	},
}
