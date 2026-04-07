// exec_safety.go provides safety checks for shell command execution.
//
// While Deneb is single-user (no multi-tenant security concerns), these
// checks protect against accidental destructive operations and help the
// LLM understand when it's about to do something irreversible.
//
// Inspired by Claude Code's BashTool security module (18 files), but
// scoped to the subset relevant for single-user deployment.
package tools

import (
	"regexp"
	"strings"
)

// DestructiveCheck describes a potentially destructive command pattern.
type DestructiveCheck struct {
	Pattern     *regexp.Regexp
	Description string
	Severity    string // "warning" or "danger"
}

// destructivePatterns detects commands that could cause data loss.
var destructivePatterns = []DestructiveCheck{
	{
		Pattern:     regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*|(-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*)|-[a-zA-Z]*r\s+-[a-zA-Z]*f|-[a-zA-Z]*f\s+-[a-zA-Z]*r)\b`),
		Description: "recursive force delete (rm -rf)",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`\bgit\s+(reset\s+--hard|clean\s+(-[a-zA-Z]*f|-[a-zA-Z]+\s+-[a-zA-Z]*f)|checkout\s+--\s+\.)`),
		Description: "destructive git operation",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`\bgit\s+push\s+.*--force\b`),
		Description: "force push",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`\b(mkfs|dd\s+if=|fdisk|parted)\b`),
		Description: "disk/partition operation",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`>\s*/dev/(sd[a-z]|nvme|vd[a-z])`),
		Description: "writing to block device",
		Severity:    "danger",
	},
	{
		Pattern:     regexp.MustCompile(`\bchmod\s+-R\s+777\b`),
		Description: "recursive world-writable permissions",
		Severity:    "warning",
	},
	{
		Pattern:     regexp.MustCompile(`\b(kill|killall|pkill)\s+-9\b`),
		Description: "force kill (SIGKILL)",
		Severity:    "warning",
	},
	{
		Pattern:     regexp.MustCompile(`\bsudo\s+rm\b`),
		Description: "sudo rm",
		Severity:    "warning",
	},
}

// CheckDestructiveCommand returns warnings for potentially destructive
// commands. Returns nil if the command appears safe.
func CheckDestructiveCommand(command string) []DestructiveCheck {
	var matches []DestructiveCheck
	for _, check := range destructivePatterns {
		if check.Pattern.MatchString(command) {
			// --force-with-lease is a safer alternative to --force; exclude it.
			if check.Description == "force push" && strings.Contains(command, "--force-with-lease") {
				continue
			}
			matches = append(matches, check)
		}
	}
	return matches
}

// FormatDestructiveWarnings returns a human-readable warning string
// for destructive command detections.
func FormatDestructiveWarnings(checks []DestructiveCheck) string {
	if len(checks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("⚠ Destructive command detected:\n")
	for _, c := range checks {
		sb.WriteString("  - ")
		sb.WriteString(c.Description)
		sb.WriteString(" [")
		sb.WriteString(c.Severity)
		sb.WriteString("]\n")
	}
	return sb.String()
}

// sedModifiesFile detects sed commands that modify files in-place.
// Returns true if the sed command uses -i flag (in-place editing).
func sedModifiesFile(command string) bool {
	// Match sed -i or sed --in-place patterns.
	return sedInPlacePattern.MatchString(command)
}

var sedInPlacePattern = regexp.MustCompile(`\bsed\s+(-[a-zA-Z]*i|--in-place)\b`)

// DetectFileModification checks if a command is likely to modify files.
// Returns the type of modification detected, or empty string if none.
func DetectFileModification(command string) string {
	if sedModifiesFile(command) {
		return "sed_in_place"
	}
	// Output redirection to a file.
	if redirectPattern.MatchString(command) {
		return "redirect"
	}
	// tee command (writes to file and stdout).
	if teePattern.MatchString(command) {
		return "tee"
	}
	return ""
}

var (
	redirectPattern = regexp.MustCompile(`[^|>]>\s*[^/&>]`) // > file (not >> and not > /dev/null)
	teePattern      = regexp.MustCompile(`\btee\s+[^|]`)
)
