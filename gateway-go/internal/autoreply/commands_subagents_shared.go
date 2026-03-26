// commands_subagents_shared.go — Shared types and utilities for subagent commands.
// Mirrors src/auto-reply/reply/commands-subagents/shared.ts (404 LOC).
package autoreply

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SubagentsAction represents the specific subagent command action.
type SubagentsAction string

const (
	SubagentsActionList    SubagentsAction = "list"
	SubagentsActionKill    SubagentsAction = "kill"
	SubagentsActionLog     SubagentsAction = "log"
	SubagentsActionSend    SubagentsAction = "send"
	SubagentsActionSteer   SubagentsAction = "steer"
	SubagentsActionInfo    SubagentsAction = "info"
	SubagentsActionSpawn   SubagentsAction = "spawn"
	SubagentsActionFocus   SubagentsAction = "focus"
	SubagentsActionUnfocus SubagentsAction = "unfocus"
	SubagentsActionAgents  SubagentsAction = "agents"
	SubagentsActionHelp    SubagentsAction = "help"
)

// Subagent command prefixes.
const (
	SubagentsCmdPrefix  = "/subagents"
	SubagentsCmdKill    = "/kill"
	SubagentsCmdSteer   = "/steer"
	SubagentsCmdTell    = "/tell"
	SubagentsCmdFocus   = "/focus"
	SubagentsCmdUnfocus = "/unfocus"
	SubagentsCmdAgents  = "/agents"
)

// RecentWindowMinutes is the lookback for "recent" subagent display.
const RecentWindowMinutes = 30

// SteerAbortSettleTimeoutMs is the timeout for steer abort settling.
const SteerAbortSettleTimeoutMs = 5_000

var validSubagentsActions = map[string]SubagentsAction{
	"list":    SubagentsActionList,
	"kill":    SubagentsActionKill,
	"log":     SubagentsActionLog,
	"send":    SubagentsActionSend,
	"steer":   SubagentsActionSteer,
	"info":    SubagentsActionInfo,
	"spawn":   SubagentsActionSpawn,
	"focus":   SubagentsActionFocus,
	"unfocus": SubagentsActionUnfocus,
	"agents":  SubagentsActionAgents,
	"help":    SubagentsActionHelp,
}

// SubagentRunRecord mirrors the TS SubagentRunRecord.
type SubagentRunRecord struct {
	RunID               string `json:"runId"`
	ChildSessionKey     string `json:"childSessionKey"`
	ControllerKey       string `json:"controllerSessionKey,omitempty"`
	RequesterKey        string `json:"requesterSessionKey"`
	RequesterDisplayKey string `json:"requesterDisplayKey"`
	Task                string `json:"task"`
	Cleanup             string `json:"cleanup"` // "delete" or "keep"
	Label               string `json:"label,omitempty"`
	Model               string `json:"model,omitempty"`
	WorkspaceDir        string `json:"workspaceDir,omitempty"`
	RunTimeoutSeconds   int    `json:"runTimeoutSeconds,omitempty"`
	SpawnMode           string `json:"spawnMode,omitempty"` // "run" or "session"
	CreatedAt           int64  `json:"createdAt"`
	StartedAt           int64  `json:"startedAt,omitempty"`
	SessionStartedAt    int64  `json:"sessionStartedAt,omitempty"`
	EndedAt             int64  `json:"endedAt,omitempty"`
	OutcomeStatus       string `json:"outcomeStatus,omitempty"`
	OutcomeError        string `json:"outcomeError,omitempty"`
	ArchiveAtMs         int64  `json:"archiveAtMs,omitempty"`
	CleanupHandled      bool   `json:"cleanupHandled,omitempty"`
	// Depth tracking for nested subagent hierarchies.
	SpawnDepth         int `json:"spawnDepth,omitempty"`
	PendingDescendants int `json:"pendingDescendants,omitempty"`
	// Announce state for completion message delivery.
	FrozenResultText string `json:"frozenResultText,omitempty"`
	EndedReason      string `json:"endedReason,omitempty"` // "complete", "error", "killed"
	// Accumulated runtime from prior completed runs (for session-mode restarts).
	AccumulatedRuntimeMs int64 `json:"accumulatedRuntimeMs,omitempty"`
	// Whether the run expects a completion message to be delivered.
	ExpectsCompletion bool `json:"expectsCompletionMessage,omitempty"`
}

// SubagentsCommandContext holds context for a subagent command execution.
type SubagentsCommandContext struct {
	HandledPrefix string
	RequesterKey  string
	Runs          []SubagentRunRecord
	RestTokens    []string
	// Command context.
	SessionKey string
	Channel    string
	AccountID  string
	ThreadID   string
	SenderID   string
	IsGroup    bool
}

// SubagentCommandResult holds the result of a subagent command.
type SubagentCommandResult struct {
	Reply      string
	ShouldStop bool
}

// stopWithText creates a result that stops command processing with a message.
func subagentStopWithText(text string) *SubagentCommandResult {
	return &SubagentCommandResult{Reply: text, ShouldStop: true}
}

// SubagentListItem represents one entry in the subagent list display.
type SubagentListItem struct {
	Index              int    `json:"index"`
	Line               string `json:"line"`
	RunID              string `json:"runId"`
	SessionKey         string `json:"sessionKey"`
	Label              string `json:"label"`
	Task               string `json:"task"`
	Status             string `json:"status"`
	PendingDescendants int    `json:"pendingDescendants"`
	Runtime            string `json:"runtime"`
	RuntimeMs          int64  `json:"runtimeMs"`
	Model              string `json:"model,omitempty"`
	TotalTokens        int64  `json:"totalTokens,omitempty"`
	StartedAt          int64  `json:"startedAt,omitempty"`
	EndedAt            int64  `json:"endedAt,omitempty"`
}

// ResolvedSubagentController describes the caller's permission scope.
type ResolvedSubagentController struct {
	ControllerSessionKey string
	CallerSessionKey     string
	CallerIsSubagent     bool
	ControlScope         string // "children" or "none"
}

// ResolveHandledPrefix checks if the normalized body starts with a subagent command prefix.
func ResolveHandledPrefix(normalized string) string {
	prefixes := []string{
		SubagentsCmdPrefix,
		SubagentsCmdKill,
		SubagentsCmdSteer,
		SubagentsCmdTell,
		SubagentsCmdFocus,
		SubagentsCmdUnfocus,
		SubagentsCmdAgents,
	}
	for _, p := range prefixes {
		if strings.HasPrefix(normalized, p) {
			return p
		}
	}
	return ""
}

// ResolveSubagentsAction determines which subagent action to run.
func ResolveSubagentsAction(handledPrefix string, restTokens []string) (SubagentsAction, []string) {
	switch handledPrefix {
	case SubagentsCmdPrefix:
		action := "list"
		if len(restTokens) > 0 {
			action = strings.ToLower(restTokens[0])
		}
		if a, ok := validSubagentsActions[action]; ok {
			// Consume the action token.
			if len(restTokens) > 0 {
				restTokens = restTokens[1:]
			}
			return a, restTokens
		}
		return "", restTokens
	case SubagentsCmdKill:
		return SubagentsActionKill, restTokens
	case SubagentsCmdFocus:
		return SubagentsActionFocus, restTokens
	case SubagentsCmdUnfocus:
		return SubagentsActionUnfocus, restTokens
	case SubagentsCmdAgents:
		return SubagentsActionAgents, restTokens
	case SubagentsCmdSteer, SubagentsCmdTell:
		return SubagentsActionSteer, restTokens
	}
	return "", restTokens
}

// FormatRunLabel returns a display label for a subagent run.
func FormatRunLabel(run SubagentRunRecord) string {
	if run.Label != "" {
		return run.Label
	}
	if run.RunID != "" && len(run.RunID) >= 8 {
		return run.RunID[:8]
	}
	return run.RunID
}

// FormatRunStatus returns a human-readable status string for a subagent run.
func FormatRunStatus(run SubagentRunRecord) string {
	if run.EndedAt == 0 {
		return "running"
	}
	if run.OutcomeStatus != "" {
		return run.OutcomeStatus
	}
	return "done"
}

// ResolveDisplayStatus returns a display status including pending descendants.
func ResolveDisplayStatus(run SubagentRunRecord, pendingDescendants int) string {
	if pendingDescendants > 0 {
		childLabel := "children"
		if pendingDescendants == 1 {
			childLabel = "child"
		}
		return fmt.Sprintf("active (waiting on %d %s)", pendingDescendants, childLabel)
	}
	status := FormatRunStatus(run)
	if status == "error" {
		return "failed"
	}
	return status
}

// FormatDurationCompact formats milliseconds into a compact human-readable duration.
func FormatDurationCompact(ms int64) string {
	if ms <= 0 {
		return "0s"
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

// SortSubagentRuns sorts runs with active first, then by creation time descending.
func SortSubagentRuns(runs []SubagentRunRecord) []SubagentRunRecord {
	sorted := make([]SubagentRunRecord, len(runs))
	copy(sorted, runs)
	sort.Slice(sorted, func(i, j int) bool {
		iActive := sorted[i].EndedAt == 0
		jActive := sorted[j].EndedAt == 0
		if iActive != jActive {
			return iActive
		}
		return sorted[i].CreatedAt > sorted[j].CreatedAt
	})
	return sorted
}

// TruncateLine truncates a string to maxLen with "..." suffix.
func TruncateLine(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// ResolveSubagentTarget resolves a run from a target token (index, label, runId prefix, session key).
func ResolveSubagentTarget(runs []SubagentRunRecord, token string) (*SubagentRunRecord, string) {
	if token == "" {
		return nil, "Missing subagent id."
	}

	// Try numeric index (1-based).
	sorted := SortSubagentRuns(runs)
	if len(token) <= 3 {
		idx := 0
		isNum := true
		for _, c := range token {
			if c < '0' || c > '9' {
				isNum = false
				break
			}
			idx = idx*10 + int(c-'0')
		}
		if isNum && idx >= 1 && idx <= len(sorted) {
			return &sorted[idx-1], ""
		}
		if isNum {
			return nil, fmt.Sprintf("Invalid subagent index: %s", token)
		}
	}

	// Try prefix on runId.
	var runIdMatches []*SubagentRunRecord
	for i := range sorted {
		if strings.HasPrefix(sorted[i].RunID, token) {
			runIdMatches = append(runIdMatches, &sorted[i])
		}
	}
	if len(runIdMatches) == 1 {
		return runIdMatches[0], ""
	}
	if len(runIdMatches) > 1 {
		return nil, fmt.Sprintf("Ambiguous run id prefix: %s", token)
	}

	// Try exact session key.
	for i := range sorted {
		if sorted[i].ChildSessionKey == token {
			return &sorted[i], ""
		}
	}

	// Try label match (case-insensitive).
	lowered := strings.ToLower(token)
	var labelExact []*SubagentRunRecord
	var labelPrefix []*SubagentRunRecord
	for i := range sorted {
		label := strings.ToLower(sorted[i].Label)
		if label == lowered {
			labelExact = append(labelExact, &sorted[i])
		} else if strings.HasPrefix(label, lowered) {
			labelPrefix = append(labelPrefix, &sorted[i])
		}
	}
	if len(labelExact) == 1 {
		return labelExact[0], ""
	}
	if len(labelExact) > 1 {
		return nil, fmt.Sprintf("Ambiguous subagent label: %s", token)
	}
	if len(labelPrefix) == 1 {
		return labelPrefix[0], ""
	}
	if len(labelPrefix) > 1 {
		return nil, fmt.Sprintf("Ambiguous subagent label prefix: %s", token)
	}

	return nil, fmt.Sprintf("Unknown subagent id: %s", token)
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
