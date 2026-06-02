package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Truncate shortens s to maxLen runes, appending "..." if truncated.
func Truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// --- unified sessions tool ---

// ToolSessions creates the unified sessions tool with action dispatch (list/history/search/send).
func ToolSessions(d *toolctx.SessionDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		switch p.Action {
		case "list":
			return toolSessionsList(d.Manager)(ctx, input)
		case "history":
			return toolSessionsHistory(d.Transcript)(ctx, input)
		case "search":
			return toolSessionsSearch(d.Transcript)(ctx, input)
		case "send":
			return toolSessionsSend(d)(ctx, input)
		default:
			return "action은 list, history, search, send 중 하나를 지정하세요.", nil
		}
	}
}

// --- sessions list sub-action ---

// toolSessionsList returns a tool function that lists active sessions.
func toolSessionsList(sessions *session.Manager) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		currentKey := toolctx.SessionKeyFromContext(ctx)

		if sessions == nil {
			return fmt.Sprintf("Current session: %s\nSession manager not available.", currentKey), nil
		}

		var p struct {
			Limit int      `json:"limit"`
			Kinds []string `json:"kinds"`
		}
		if len(input) > 0 {
			_ = json.Unmarshal(input, &p) // best-effort: use defaults on parse failure
		}

		allSessions := sessions.List()
		if len(allSessions) == 0 {
			return fmt.Sprintf("Current session: %s\nNo other sessions found.", currentKey), nil
		}

		// Apply kind filter if specified.
		kindFilter := make(map[string]struct{}, len(p.Kinds))
		for _, k := range p.Kinds {
			kindFilter[k] = struct{}{}
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Sessions (%d total):\n\n", len(allSessions))
		shown := 0
		limit := p.Limit
		if limit <= 0 {
			limit = 50
		}
		for _, s := range allSessions {
			if _, ok := kindFilter[string(s.Kind)]; len(kindFilter) > 0 && !ok {
				continue
			}
			if shown >= limit {
				break
			}
			marker := ""
			if s.Key == currentKey {
				marker = " ← current"
			}
			fmt.Fprintf(&sb, "- **%s** (kind=%s, status=%s, model=%s)%s\n",
				s.Key, s.Kind, s.Status, s.Model, marker)
			shown++
		}
		return sb.String(), nil
	}
}

// --- sessions history sub-action ---

// toolSessionsHistory returns a tool function that retrieves session transcript history.
func toolSessionsHistory(transcript toolctx.TranscriptStore) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SessionKey string `json:"sessionKey"`
			Limit      int    `json:"limit"`
		}
		if err := jsonutil.UnmarshalInto("sessions_history params", input, &p); err != nil {
			return "", err
		}
		if p.SessionKey == "" {
			return "", fmt.Errorf("sessionKey is required")
		}
		if transcript == nil {
			return "Transcript store not available. Cannot fetch session history.", nil
		}

		limit := p.Limit
		if limit <= 0 {
			limit = 20
		}

		msgs, total, err := transcript.Load(p.SessionKey, limit)
		if err != nil {
			return fmt.Sprintf("Failed to load history for session %q: %s", p.SessionKey, err.Error()), nil
		}
		if len(msgs) == 0 {
			return fmt.Sprintf("Session %q has no history (or does not exist).", p.SessionKey), nil
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Session %q history (%d of %d messages):\n\n", p.SessionKey, len(msgs), total)
		for i, msg := range msgs {
			content := msg.TextContent()
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, msg.Role, content)
		}
		return sb.String(), nil
	}
}

// --- sessions search sub-action ---

// toolSessionsSearch returns a tool function that searches across session transcripts.
func toolSessionsSearch(transcript toolctx.TranscriptStore) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Query      string `json:"query"`
			MaxResults int    `json:"maxResults"`
		}
		if err := jsonutil.UnmarshalInto("sessions_search params", input, &p); err != nil {
			return "", err
		}
		if p.Query == "" {
			return "", fmt.Errorf("query is required")
		}
		if transcript == nil {
			return "Transcript store not available.", nil
		}

		maxResults := p.MaxResults
		if maxResults <= 0 {
			maxResults = 20
		}
		if maxResults > 100 {
			maxResults = 100
		}

		results, err := transcript.Search(p.Query, maxResults)
		if err != nil {
			return fmt.Sprintf("Search failed: %s", err.Error()), nil
		}
		if len(results) == 0 {
			return fmt.Sprintf("No matches found for %q across session transcripts.", p.Query), nil
		}

		var sb strings.Builder
		totalMatches := 0
		for _, r := range results {
			totalMatches += len(r.Matches)
		}
		fmt.Fprintf(&sb, "Found %d match(es) across %d session(s) for %q:\n\n", totalMatches, len(results), p.Query)

		for _, r := range results {
			fmt.Fprintf(&sb, "### Session: %s\n", r.SessionKey)
			for _, m := range r.Matches {
				// Context layout: [before, after] when both exist,
				// [after] when index==0, [before] when last message.
				hasBefore := m.Index > 0 && len(m.Context) > 0
				hasAfter := len(m.Context) > 1 || (len(m.Context) == 1 && !hasBefore)

				if hasBefore {
					c := m.Context[0]
					content := Truncate(c.TextContent(), 200)
					fmt.Fprintf(&sb, "  [ctx] [%s] %s\n", c.Role, content)
				}

				fmt.Fprintf(&sb, "  **[%s]** %s\n", m.Message.Role, Truncate(m.Message.TextContent(), 500))

				if hasAfter {
					c := m.Context[len(m.Context)-1]
					content := Truncate(c.TextContent(), 200)
					fmt.Fprintf(&sb, "  [ctx] [%s] %s\n", c.Role, content)
				}
				sb.WriteString("\n")
			}
		}
		return sb.String(), nil
	}
}

// --- sessions send sub-action ---

// toolSessionsSend returns a tool function that sends a message to another session.
func toolSessionsSend(d *toolctx.SessionDeps) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SessionKey string `json:"sessionKey"`
			Message    string `json:"message"`
		}
		if err := jsonutil.UnmarshalInto("sessions_send params", input, &p); err != nil {
			return "", err
		}
		if p.Message == "" {
			return "", fmt.Errorf("message is required")
		}

		targetKey := p.SessionKey
		if targetKey == "" {
			targetKey = "main"
		}

		if d == nil || d.SendFn == nil {
			return "Cross-session messaging is not available (session send function not wired).", nil
		}

		if err := d.SendFn(targetKey, p.Message); err != nil {
			return fmt.Sprintf("Failed to send message to session %q: %s", targetKey, err.Error()), nil
		}
		return fmt.Sprintf("Message sent to session %q.", targetKey), nil
	}
}

// --- sessions_spawn tool ---

// ToolSessionsSpawn returns a tool function that spawns a sub-agent session.
func ToolSessionsSpawn(d *toolctx.SessionDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Task       string `json:"task"`
			Label      string `json:"label"`
			Model      string `json:"model"`       // role name: "main","lightweight","fallback"
			ToolPreset string `json:"tool_preset"` // tool preset: "researcher","implementer","verifier"
		}
		if err := jsonutil.UnmarshalInto("sessions_spawn params", input, &p); err != nil {
			return "", err
		}
		if p.Task == "" {
			return "", fmt.Errorf("task is required")
		}

		if d == nil || d.Manager == nil || d.SendFn == nil {
			return "Sub-agent spawning is not available (session dependencies not wired).", nil
		}

		// Create a unique session key for the sub-agent.
		parentKey := toolctx.SessionKeyFromContext(ctx)
		label := p.Label
		if label == "" {
			label = "subagent"
		}
		childKey := fmt.Sprintf("%s:%s:%d", parentKey, label, time.Now().UnixMilli())

		// Create the child session.
		childSession := d.Manager.Create(childKey, session.KindDirect)
		if p.Model != "" {
			childSession.Model = p.Model
		} else if d.SubagentDefaultModel != "" {
			childSession.Model = d.SubagentDefaultModel
		}
		childSession.SpawnedBy = parentKey
		childSession.ToolPreset = p.ToolPreset
		d.Manager.Set(childSession)

		// Send the task message to the child session.
		if err := d.SendFn(childKey, p.Task); err != nil {
			return fmt.Sprintf("Sub-agent session %q created but failed to send task: %s", childKey, err.Error()), nil
		}

		// Signal the executor that a sub-agent was spawned in this run.
		if flag := toolctx.SpawnFlagFromContext(ctx); flag != nil {
			flag.Set()
		}

		result := fmt.Sprintf("Sub-agent spawned.\nSession: %s\nTask: %s", childKey, p.Task)
		if p.ToolPreset != "" {
			result += fmt.Sprintf("\nTool preset: %s", p.ToolPreset)
		}
		return result, nil
	}
}
