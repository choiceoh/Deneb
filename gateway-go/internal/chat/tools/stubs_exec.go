package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
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

// --- cron tool ---

// ToolCron returns a tool function that manages cron jobs.
func ToolCron(d *toolctx.ChronoDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action   string         `json:"action"`
			JobID    string         `json:"jobId"`
			Job      map[string]any `json:"job"`
			Name     string         `json:"name"`
			Schedule string         `json:"schedule"`
			Command  string         `json:"command"`
			Text     string         `json:"text"`
		}
		if err := jsonutil.UnmarshalInto("cron params", input, &p); err != nil {
			return "", err
		}

		cronSched := d.Scheduler
		if cronSched == nil {
			return "Cron scheduler not available.", nil
		}

		switch p.Action {
		case "status":
			running := cronSched.Running()
			nextRun := cronSched.NextRunAt()
			taskCount := len(cronSched.List())
			return fmt.Sprintf("Cron status: %d jobs, running=%v, nextRunAtMs=%d", taskCount, running, nextRun), nil

		case "list":
			jobs := cronSched.List()
			if len(jobs) == 0 {
				return "No cron jobs configured.", nil
			}
			data, _ := json.MarshalIndent(jobs, "", "  ")
			return string(data), nil

		case "add":
			name := p.Name
			schedule := p.Schedule
			command := p.Command
			// Support nested job object as well.
			if p.Job != nil {
				if v, ok := p.Job["name"].(string); ok && name == "" {
					name = v
				}
				if v, ok := p.Job["schedule"].(string); ok && schedule == "" {
					schedule = v
				}
				if v, ok := p.Job["command"].(string); ok && command == "" {
					command = v
				}
			}
			if name == "" || schedule == "" || command == "" {
				return "", fmt.Errorf("name, schedule, and command are required for add")
			}
			sched, err := cron.ParseSchedule(schedule)
			if err != nil {
				return "", fmt.Errorf("invalid schedule: %w", err)
			}
			sched.Label = name
			// Build real execution callback that sends the command to a session
			// or falls back to direct shell execution.
			cronCommand := command
			cronName := name
			cronCallback := func(runCtx context.Context) error {
				if d != nil && d.SendFn != nil {
					return d.SendFn("cron:"+cronName, cronCommand)
				}
				// Fallback: execute as shell command directly.
				cmd := exec.CommandContext(runCtx, "bash", "-c", cronCommand)
				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("cron exec failed: %w\n%s", err, string(out))
				}
				return nil
			}
			if regErr := cronSched.Register(ctx, name, sched, cronCallback); regErr != nil {
				return "", fmt.Errorf("failed to register cron job: %w", regErr)
			}
			return fmt.Sprintf("Cron job %q added (schedule: %s).", name, schedule), nil

		case "update":
			id := p.JobID
			if id == "" {
				return "", fmt.Errorf("jobId is required for update")
			}
			patch := p.Job
			if patch == nil {
				return "", fmt.Errorf("job patch object is required for update")
			}
			if err := cronSched.Update(id, patch); err != nil {
				return "", fmt.Errorf("update failed: %w", err)
			}
			st := cronSched.Get(id)
			data, _ := json.MarshalIndent(st, "", "  ")
			return fmt.Sprintf("Cron job %q updated.\n%s", id, string(data)), nil

		case "remove":
			id := p.JobID
			if id == "" {
				return "", fmt.Errorf("jobId is required for remove")
			}
			removed := cronSched.Unregister(id)
			if !removed {
				return fmt.Sprintf("Cron job %q not found.", id), nil
			}
			return fmt.Sprintf("Cron job %q removed.", id), nil

		case "run":
			id := p.JobID
			if id == "" {
				return "", fmt.Errorf("jobId is required for run")
			}
			result, err := cronSched.RunNow(ctx, id)
			if err != nil {
				return "", fmt.Errorf("run failed: %w", err)
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			return string(data), nil

		case "wake":
			return fmt.Sprintf("Wake event: %s", p.Text), nil

		default:
			return fmt.Sprintf("Unknown cron action: %q. Supported: status, list, add, update, remove, run, wake", p.Action), nil
		}
	}
}

// --- sessions_list tool ---

// ToolSessionsList returns a tool function that lists active sessions.
func ToolSessionsList(sessions *session.Manager) ToolFunc {
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
			_ = json.Unmarshal(input, &p)
		}

		allSessions := sessions.List()
		if len(allSessions) == 0 {
			return fmt.Sprintf("Current session: %s\nNo other sessions found.", currentKey), nil
		}

		// Apply kind filter if specified.
		kindFilter := make(map[string]bool, len(p.Kinds))
		for _, k := range p.Kinds {
			kindFilter[k] = true
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "Sessions (%d total):\n\n", len(allSessions))
		shown := 0
		limit := p.Limit
		if limit <= 0 {
			limit = 50
		}
		for _, s := range allSessions {
			if len(kindFilter) > 0 && !kindFilter[string(s.Kind)] {
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

// --- sessions_history tool ---

// ToolSessionsHistory returns a tool function that retrieves session transcript history.
func ToolSessionsHistory(transcript toolctx.TranscriptStore) ToolFunc {
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

// --- sessions_search tool ---

// ToolSessionsSearch returns a tool function that searches across session transcripts.
func ToolSessionsSearch(transcript toolctx.TranscriptStore) ToolFunc {
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

// --- sessions_send tool ---

// ToolSessionsSend returns a tool function that sends a message to another session.
func ToolSessionsSend(d *toolctx.SessionDeps) ToolFunc {
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
			Model      string `json:"model"`       // role name: "main","lightweight","pilot","fallback"
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

		result := fmt.Sprintf("Sub-agent spawned.\nSession: %s\nTask: %s", childKey, p.Task)
		if p.ToolPreset != "" {
			result += fmt.Sprintf("\nTool preset: %s", p.ToolPreset)
		}
		return result, nil
	}
}

// --- subagents tool ---
