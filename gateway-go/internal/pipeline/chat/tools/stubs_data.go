package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// ToolSubagents returns a tool that manages subagent sessions: listing active agents,
// sending messages, and waiting for results via d.
func ToolSubagents(d *toolctx.SessionDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action  string `json:"action"`
			Target  string `json:"target"`
			Message string `json:"message"`
		}
		if err := jsonutil.UnmarshalInto("subagents params", input, &p); err != nil {
			return "", err
		}
		if p.Action == "" {
			p.Action = "list"
		}

		if d == nil || d.Manager == nil {
			return "Sub-agent management not available (session dependencies not wired).", nil
		}

		parentKey := toolctx.SessionKeyFromContext(ctx)

		// Gather children: sessions where SpawnedBy == parentKey.
		allSessions := d.Manager.List()
		var children []*session.Session
		for _, s := range allSessions {
			if s.SpawnedBy == parentKey {
				children = append(children, s)
			}
		}

		// Sort: running first, then by UpdatedAt descending.
		sort.Slice(children, func(i, j int) bool {
			iRunning := children[i].Status == session.StatusRunning
			jRunning := children[j].Status == session.StatusRunning
			if iRunning != jRunning {
				return iRunning
			}
			return children[i].UpdatedAt > children[j].UpdatedAt
		})

		switch p.Action {
		case "list":
			return subagentsList(children), nil
		case "kill":
			return subagentsKill(d, children, p.Target)
		case "steer":
			return subagentsSteer(d, children, p.Target, p.Message)
		default:
			return fmt.Sprintf("Unknown subagents action: %q", p.Action), nil
		}
	}
}

// subagentsList formats the children list for display.
func subagentsList(children []*session.Session) string {
	if len(children) == 0 {
		return "No sub-agents."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Sub-agents (%d):\n", len(children))
	for i, c := range children {
		label := c.Label
		if label == "" {
			label = c.Key
		}
		status := string(c.Status)
		if status == "" {
			status = "unknown"
		}

		var parts []string
		// Runtime.
		if c.RuntimeMs != nil {
			parts = append(parts, textutil.FormatDuration(*c.RuntimeMs))
		} else if c.Status == session.StatusRunning && c.StartedAt != nil {
			elapsed := time.Now().UnixMilli() - *c.StartedAt
			parts = append(parts, textutil.FormatDuration(elapsed))
		}
		// Tokens.
		if c.TotalTokens != nil && *c.TotalTokens > 0 {
			parts = append(parts, fmt.Sprintf("%dtok", *c.TotalTokens))
		}
		// Model.
		if c.Model != "" {
			parts = append(parts, fmt.Sprintf("model=%s", c.Model))
		}

		fmt.Fprintf(&sb, "  %d. [%s] %s", i+1, status, truncateLine(label, 60))
		if len(parts) > 0 {
			fmt.Fprintf(&sb, " (%s)", strings.Join(parts, ", "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func truncateLine(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return string(r[:maxLen-1]) + "…"
}

// subagentsKill kills one or all child sessions.
func subagentsKill(d *toolctx.SessionDeps, children []*session.Session, target string) (string, error) {
	if target == "" {
		return "Target is required. Use a sub-agent index, label, session key, or \"all\".", nil
	}

	if strings.ToLower(target) == "all" {
		killed := 0
		for _, c := range children {
			if c.Status == session.StatusRunning {
				killSession(d.Manager, c)
				killed++
			}
		}
		if killed == 0 {
			return "No running sub-agents to kill.", nil
		}
		return fmt.Sprintf("Killed %d sub-agent(s).", killed), nil
	}

	child, errMsg := resolveChildTarget(children, target)
	if errMsg != "" {
		return errMsg, nil
	}
	if child.Status != session.StatusRunning {
		return fmt.Sprintf("Sub-agent %q is not running (status: %s).", child.Key, child.Status), nil
	}
	killSession(d.Manager, child)
	return fmt.Sprintf("Killed sub-agent: %s", child.Key), nil
}

// subagentsSteer sends a steering message to a running child session.
func subagentsSteer(d *toolctx.SessionDeps, children []*session.Session, target, message string) (string, error) {
	if d.SendFn == nil {
		return "Steering not available (SessionSendFn not wired).", nil
	}
	if message == "" {
		return "Message is required for steer action.", nil
	}

	// Auto-target if exactly one running child and no target specified.
	if target == "" {
		var running []*session.Session
		for _, c := range children {
			if c.Status == session.StatusRunning {
				running = append(running, c)
			}
		}
		switch len(running) {
		case 0:
			return "No running sub-agents to steer.", nil
		case 1:
			target = running[0].Key
		default:
			return "Multiple running sub-agents. Specify a target (index, label, or key).", nil
		}
	}

	child, errMsg := resolveChildTarget(children, target)
	if errMsg != "" {
		return errMsg, nil
	}
	if child.Status != session.StatusRunning {
		return fmt.Sprintf("Sub-agent %q is not running (status: %s).", child.Key, child.Status), nil
	}

	if err := d.SendFn(child.Key, message); err != nil {
		return fmt.Sprintf("Failed to steer sub-agent %q: %s", child.Key, err.Error()), nil
	}
	return fmt.Sprintf("Steered sub-agent: %s\nMessage: %s", child.Key, message), nil
}

// killSession applies the kill pattern (mirrors http_session_kill.go).
func killSession(sessions *session.Manager, s *session.Session) {
	now := time.Now().UnixMilli()
	s.Status = session.StatusKilled
	s.EndedAt = &now
	if s.StartedAt != nil {
		runtime := now - *s.StartedAt
		s.RuntimeMs = &runtime
	}
	s.UpdatedAt = now
	_ = sessions.Set(s) // RUNNING → KILLED is always valid; error unreachable
}

// resolveChildTarget finds a child by 1-based index, exact key, label, or key prefix.
func resolveChildTarget(children []*session.Session, target string) (child *session.Session, errMsg string) {
	if target == "" {
		return nil, "Missing sub-agent target."
	}

	// Try numeric index (1-based).
	if len(target) <= 3 {
		idx := 0
		isNum := true
		for _, c := range target {
			if c < '0' || c > '9' {
				isNum = false
				break
			}
			idx = idx*10 + int(c-'0')
		}
		if isNum && idx >= 1 && idx <= len(children) {
			return children[idx-1], ""
		}
		if isNum {
			return nil, fmt.Sprintf("Invalid sub-agent index: %s (have %d sub-agents)", target, len(children))
		}
	}

	// Try exact session key.
	for _, c := range children {
		if c.Key == target {
			return c, ""
		}
	}

	// Try label match (case-insensitive exact, then prefix).
	lowered := strings.ToLower(target)
	var exactLabel []*session.Session
	var prefixLabel []*session.Session
	for _, c := range children {
		l := strings.ToLower(c.Label)
		if l == lowered {
			exactLabel = append(exactLabel, c)
		} else if strings.HasPrefix(l, lowered) {
			prefixLabel = append(prefixLabel, c)
		}
	}
	if len(exactLabel) == 1 {
		return exactLabel[0], ""
	}
	if len(exactLabel) > 1 {
		return nil, fmt.Sprintf("Ambiguous sub-agent label: %s", target)
	}
	if len(prefixLabel) == 1 {
		return prefixLabel[0], ""
	}
	if len(prefixLabel) > 1 {
		return nil, fmt.Sprintf("Ambiguous sub-agent label prefix: %s", target)
	}

	// Try session key prefix.
	var keyPrefix []*session.Session
	for _, c := range children {
		if strings.HasPrefix(c.Key, target) {
			keyPrefix = append(keyPrefix, c)
		}
	}
	if len(keyPrefix) == 1 {
		return keyPrefix[0], ""
	}
	if len(keyPrefix) > 1 {
		return nil, fmt.Sprintf("Ambiguous sub-agent key prefix: %s", target)
	}

	return nil, fmt.Sprintf("Unknown sub-agent: %s", target)
}

// --- session_status tool ---
