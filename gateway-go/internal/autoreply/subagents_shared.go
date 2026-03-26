// subagents_shared.go — Shared subagent command infrastructure.
// Mirrors src/auto-reply/reply/commands-subagents/shared.ts (403 LOC).
package autoreply

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Subagent command prefixes.
const (
	SubagentsCommand = "/subagents"
	KillCommand      = "/kill"
	SteerCommand     = "/steer"
	TellCommand      = "/tell"
	FocusCommand     = "/focus"
	UnfocusCommand   = "/unfocus"
	AgentsCommand    = "/agents"
)

// SubagentsAction defines the possible subagent actions.
type SubagentsAction string

const (
	ActionList    SubagentsAction = "list"
	ActionKill    SubagentsAction = "kill"
	ActionLog     SubagentsAction = "log"
	ActionSend    SubagentsAction = "send"
	ActionSteer   SubagentsAction = "steer"
	ActionInfo    SubagentsAction = "info"
	ActionSpawn   SubagentsAction = "spawn"
	ActionFocus   SubagentsAction = "focus"
	ActionUnfocus SubagentsAction = "unfocus"
	ActionAgents  SubagentsAction = "agents"
	ActionHelp    SubagentsAction = "help"
)

// ValidSubagentsActions is the set of recognized subagent actions.
var ValidSubagentsActions = map[SubagentsAction]bool{
	ActionList: true, ActionKill: true, ActionLog: true,
	ActionSend: true, ActionSteer: true, ActionInfo: true,
	ActionSpawn: true, ActionFocus: true, ActionUnfocus: true,
	ActionAgents: true, ActionHelp: true,
}

// RecentWindowMinutes is the default time window for "recent" subagent runs.
const RecentWindowMinutes = 30

// SteerAbortSettleTimeoutMs is the grace period for steer-restart abort to settle.
const SteerAbortSettleTimeoutMs = 5_000

// SubagentsCommandContext provides the context for subagent command execution.
type SubagentsCommandContext struct {
	Params        CommandContext
	HandledPrefix string
	RequesterKey  string
	Runs          []*SubagentRunRecord
	RestTokens    []string
}

// ResolveDisplayStatus returns a human-readable status with pending descendant info.
func ResolveDisplayStatus(entry *SubagentRunRecord, pendingDescendants int) string {
	pending := int(math.Max(0, float64(pendingDescendants)))
	if pending > 0 {
		childLabel := "children"
		if pending == 1 {
			childLabel = "child"
		}
		return fmt.Sprintf("active (waiting on %d %s)", pending, childLabel)
	}
	status := FormatRunStatus(entry)
	if status == "error" {
		return "failed"
	}
	return status
}

// ResolveHandledPrefix determines which command prefix triggered the handler.
func ResolveHandledPrefix(normalized string) string {
	lower := strings.ToLower(strings.TrimSpace(normalized))
	prefixes := []string{
		SubagentsCommand, KillCommand, SteerCommand,
		TellCommand, FocusCommand, UnfocusCommand, AgentsCommand,
	}
	for _, p := range prefixes {
		if strings.HasPrefix(lower, p) {
			rest := lower[len(p):]
			if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
				return p
			}
		}
	}
	return ""
}

// ResolveSubagentsAction determines the action from the command prefix and tokens.
func ResolveSubagentsAction(handledPrefix string, restTokens []string) (SubagentsAction, []string) {
	switch handledPrefix {
	case SubagentsCommand:
		if len(restTokens) == 0 {
			return ActionList, restTokens
		}
		action := SubagentsAction(strings.ToLower(restTokens[0]))
		if !ValidSubagentsActions[action] {
			return "", restTokens
		}
		return action, restTokens[1:]
	case KillCommand:
		return ActionKill, restTokens
	case FocusCommand:
		return ActionFocus, restTokens
	case UnfocusCommand:
		return ActionUnfocus, restTokens
	case AgentsCommand:
		return ActionAgents, restTokens
	case SteerCommand, TellCommand:
		return ActionSteer, restTokens
	default:
		return "", restTokens
	}
}

// StopWithText creates a CommandResult that stops processing with text.
func StopWithText(text string) *CommandResult {
	return &CommandResult{Reply: text, SkipAgent: true}
}

// StopWithError creates a CommandResult that stops with an error.
func StopWithError(text string) *CommandResult {
	return &CommandResult{Reply: "⚠️ " + text, SkipAgent: true, IsError: true}
}

// ResolveSubagentTarget resolves a subagent target using default parameters.
func ResolveSubagentTarget(runs []*SubagentRunRecord, token string) SubagentTargetResolution {
	return ResolveSubagentTargetFromRuns(
		runs, token, RecentWindowMinutes,
		func(e *SubagentRunRecord) string { return FormatRunLabel(e, 0) },
		nil, // default isActive (endedAt == nil)
		SubagentTargetErrors{
			MissingTarget:     "Missing subagent id.",
			InvalidIndex:      func(v string) string { return fmt.Sprintf("Invalid subagent index: %s", v) },
			UnknownSession:    func(v string) string { return fmt.Sprintf("Unknown subagent session: %s", v) },
			AmbiguousLabel:    func(v string) string { return fmt.Sprintf("Ambiguous subagent label: %s", v) },
			AmbiguousLabelPfx: func(v string) string { return fmt.Sprintf("Ambiguous subagent label prefix: %s", v) },
			AmbiguousRunIDPfx: func(v string) string { return fmt.Sprintf("Ambiguous run id prefix: %s", v) },
			UnknownTarget:     func(v string) string { return fmt.Sprintf("Unknown subagent id: %s", v) },
		},
	)
}

// ResolveSubagentEntryForToken resolves and returns the entry, or an error result.
func ResolveSubagentEntryForToken(runs []*SubagentRunRecord, token string) (*SubagentRunRecord, *CommandResult) {
	resolved := ResolveSubagentTarget(runs, token)
	if resolved.Entry == nil {
		return nil, StopWithError(resolved.Error)
	}
	return resolved.Entry, nil
}

// FocusTargetResolution holds the result of resolving a focus target.
type FocusTargetResolution struct {
	TargetKind       string `json:"targetKind"` // "subagent" or "acp"
	TargetSessionKey string `json:"targetSessionKey"`
	AgentID          string `json:"agentId"`
	Label            string `json:"label,omitempty"`
}

// ChatMessage represents a message in a chat log.
type ChatMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// FormatLogLines formats chat messages for display in a log view.
func FormatLogLines(messages []ChatMessage) []string {
	var lines []string
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		label := "User"
		if msg.Role == "assistant" {
			label = "Assistant"
		}
		lines = append(lines, label+": "+text)
	}
	return lines
}

// FormatDurationCompact formats a duration in milliseconds to a compact string.
func FormatDurationCompact(ms int64) string {
	if ms <= 0 {
		return "n/a"
	}
	d := time.Duration(ms) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", ms)
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// BuildSubagentsHelp returns the help text for subagent commands.
func BuildSubagentsHelp() string {
	return strings.Join([]string{
		"Subagents",
		"Usage:",
		"- /subagents list",
		"- /subagents kill <id|#|all>",
		"- /subagents log <id|#> [limit] [tools]",
		"- /subagents info <id|#>",
		"- /subagents send <id|#> <message>",
		"- /subagents steer <id|#> <message>",
		"- /subagents spawn <agentId> <task> [--model <model>] [--thinking <level>]",
		"- /focus <subagent-label|session-key|session-id|session-label>",
		"- /unfocus",
		"- /agents",
		"- /session idle <duration|off>",
		"- /session max-age <duration|off>",
		"- /kill <id|#|all>",
		"- /steer <id|#> <message>",
		"- /tell <id|#> <message>",
		"",
		"Ids: use the list index (#), runId/session prefix, label, or full session key.",
	}, "\n")
}

// SubagentRunListEntry represents one line in the subagent list output.
type SubagentRunListEntry struct {
	Index   int
	RunID   string
	Label   string
	Status  string
	Runtime string
	Line    string
}

// BuildSubagentRunListEntries builds formatted list entries split into active and recent.
func BuildSubagentRunListEntries(runs []*SubagentRunRecord, recentMinutes, taskMaxChars int) (active, recent []SubagentRunListEntry) {
	sorted := SortSubagentRuns(runs)
	now := time.Now().UnixMilli()
	recentCutoff := now - int64(recentMinutes)*60_000

	idx := 1
	for _, r := range sorted {
		isActive := r.EndedAt == nil
		isRecent := !isActive && r.EndedAt != nil && *r.EndedAt >= recentCutoff

		if !isActive && !isRecent {
			continue
		}

		label := FormatRunLabel(r, taskMaxChars)
		status := FormatRunStatus(r)
		runtime := "n/a"
		if r.StartedAt != nil {
			endMs := now
			if r.EndedAt != nil {
				endMs = *r.EndedAt
			}
			runtime = FormatDurationCompact(endMs - *r.StartedAt)
		}

		entry := SubagentRunListEntry{
			Index:   idx,
			RunID:   r.RunID,
			Label:   label,
			Status:  status,
			Runtime: runtime,
			Line:    fmt.Sprintf("#%d [%s] %s (%s)", idx, status, label, runtime),
		}

		if isActive {
			active = append(active, entry)
		} else {
			recent = append(recent, entry)
		}
		idx++
	}
	return
}

// FormatSubagentInfo renders the info block for a single subagent run.
func FormatSubagentInfo(run *SubagentRunRecord, pendingDescendants int) string {
	now := time.Now().UnixMilli()
	runtime := "n/a"
	if run.StartedAt != nil {
		endMs := now
		if run.EndedAt != nil {
			endMs = *run.EndedAt
		}
		runtime = FormatDurationCompact(endMs - *run.StartedAt)
	}

	outcome := "n/a"
	if run.Outcome != nil {
		outcome = run.Outcome.Status
		if run.Outcome.Error != "" {
			outcome += " (" + run.Outcome.Error + ")"
		}
	}

	startedAt := "n/a"
	if run.StartedAt != nil {
		startedAt = FormatTimestampWithAge(*run.StartedAt)
	}
	endedAt := "n/a"
	if run.EndedAt != nil {
		endedAt = FormatTimestampWithAge(*run.EndedAt)
	}

	lines := []string{
		"ℹ️ Subagent info",
		fmt.Sprintf("Status: %s", ResolveDisplayStatus(run, pendingDescendants)),
		fmt.Sprintf("Label: %s", FormatRunLabel(run, 0)),
		fmt.Sprintf("Task: %s", run.Task),
		fmt.Sprintf("Run: %s", run.RunID),
		fmt.Sprintf("Session: %s", run.ChildSessionKey),
		fmt.Sprintf("Runtime: %s", runtime),
		fmt.Sprintf("Created: %s", FormatTimestampWithAge(run.CreatedAt)),
		fmt.Sprintf("Started: %s", startedAt),
		fmt.Sprintf("Ended: %s", endedAt),
		fmt.Sprintf("Cleanup: %s", run.Cleanup),
	}

	if run.ArchiveAtMs != nil {
		lines = append(lines, fmt.Sprintf("Archive: %s", FormatTimestampWithAge(*run.ArchiveAtMs)))
	}
	if run.CleanupHandled {
		lines = append(lines, "Cleanup handled: yes")
	}
	lines = append(lines, fmt.Sprintf("Outcome: %s", outcome))

	return strings.Join(lines, "\n")
}
