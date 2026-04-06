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
// When Service is available, uses persistent storage with full cron expression support.
// Falls back to basic Scheduler for in-memory operation.
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
			Enabled  *bool          `json:"enabled"`
			Limit    int            `json:"limit"`
		}
		if err := jsonutil.UnmarshalInto("cron params", input, &p); err != nil {
			return "", err
		}

		svc := d.Service
		cronSched := d.Scheduler

		if svc == nil && cronSched == nil {
			return "Cron scheduler not available.", nil
		}

		switch p.Action {
		case "status":
			return cronStatus(svc, cronSched)

		case "list":
			return cronList(svc, cronSched)

		case "add":
			return cronAdd(ctx, d, p.Name, p.Schedule, p.Command, p.Enabled, p.Job)

		case "update":
			return cronUpdate(ctx, d, p.JobID, p.Name, p.Schedule, p.Command, p.Enabled)

		case "remove":
			return cronRemove(d, p.JobID)

		case "run":
			return cronRun(ctx, d, p.JobID)

		case "get":
			return cronGet(d, p.JobID)

		case "runs":
			return cronRuns(d, p.JobID, p.Limit)

		case "wake":
			if svc != nil {
				svc.Wake(ctx, "now", p.Text)
			}
			return fmt.Sprintf("Wake event: %s", p.Text), nil

		default:
			return fmt.Sprintf("Unknown cron action: %q. Supported: status, list, add, update, remove, run, get, runs, wake", p.Action), nil
		}
	}
}

func cronStatus(svc *cron.Service, cronSched *cron.Scheduler) (string, error) {
	if svc != nil {
		st := svc.Status()
		return fmt.Sprintf("Cron service: %d jobs, running=%v, nextRunAtMs=%d",
			st.TaskCount, st.Running, st.NextRunAtMs), nil
	}
	running := cronSched.Running()
	nextRun := cronSched.NextRunAt()
	taskCount := len(cronSched.List())
	return fmt.Sprintf("Cron status: %d jobs, running=%v, nextRunAtMs=%d", taskCount, running, nextRun), nil
}

func cronList(svc *cron.Service, cronSched *cron.Scheduler) (string, error) {
	if svc != nil {
		jobs, err := svc.List(&cron.ListOptions{IncludeDisabled: true})
		if err != nil {
			return "", fmt.Errorf("list failed: %w", err)
		}
		if len(jobs) == 0 {
			return "No cron jobs configured.", nil
		}
		// Build a concise summary.
		var sb strings.Builder
		fmt.Fprintf(&sb, "%d cron job(s):\n", len(jobs))
		for _, j := range jobs {
			enabled := "enabled"
			if !j.Enabled {
				enabled = "disabled"
			}
			schedDesc := formatScheduleDesc(j.Schedule)
			nextRun := ""
			if j.State.NextRunAtMs > 0 {
				nextRun = fmt.Sprintf(", next=%s", time.UnixMilli(j.State.NextRunAtMs).Format("2006-01-02 15:04"))
			}
			sb.WriteString(fmt.Sprintf("\n- **%s** (id=%s, %s, %s%s)", j.Name, j.ID, enabled, schedDesc, nextRun))
			cmd := j.Payload.Message
			if cmd == "" {
				cmd = j.Payload.Text
			}
			if cmd != "" {
				if len(cmd) > 80 {
					cmd = cmd[:77] + "..."
				}
				sb.WriteString(fmt.Sprintf("\n  command: %s", cmd))
			}
		}
		return sb.String(), nil
	}
	// Fallback: basic scheduler.
	jobs := cronSched.List()
	if len(jobs) == 0 {
		return "No cron jobs configured.", nil
	}
	data, _ := json.MarshalIndent(jobs, "", "  ")
	return string(data), nil
}

func cronAdd(ctx context.Context, d *toolctx.ChronoDeps, name, schedule, command string, enabled *bool, jobObj map[string]any) (string, error) {
	// Support nested job object.
	if jobObj != nil {
		if v, ok := jobObj["name"].(string); ok && name == "" {
			name = v
		}
		if v, ok := jobObj["schedule"].(string); ok && schedule == "" {
			schedule = v
		}
		if v, ok := jobObj["command"].(string); ok && command == "" {
			command = v
		}
	}
	if name == "" || schedule == "" || command == "" {
		return "", fmt.Errorf("name, schedule, and command are required for add")
	}
	const maxCommandLen = 4096
	if len(command) > maxCommandLen {
		return "", fmt.Errorf("command exceeds maximum length of %d characters", maxCommandLen)
	}

	if d.Service != nil {
		storeSched, err := cron.ParseSmartSchedule(schedule)
		if err != nil {
			return "", fmt.Errorf("invalid schedule: %w", err)
		}
		isEnabled := true
		if enabled != nil {
			isEnabled = *enabled
		}
		job := cron.StoreJob{
			ID:      name,
			Name:    name,
			Enabled: isEnabled,
			Schedule: storeSched,
			Payload: cron.StorePayload{
				Kind:    "agentTurn",
				Message: command,
			},
		}
		if err := d.Service.Add(ctx, job); err != nil {
			return "", fmt.Errorf("failed to add cron job: %w", err)
		}
		schedDesc := formatScheduleDesc(storeSched)
		nextMs := cron.ComputeNextRunAtMs(storeSched, time.Now().UnixMilli())
		nextInfo := ""
		if nextMs > 0 {
			nextInfo = fmt.Sprintf(" Next run: %s.", time.UnixMilli(nextMs).Format("2006-01-02 15:04"))
		}
		return fmt.Sprintf("Cron job %q added (%s).%s", name, schedDesc, nextInfo), nil
	}

	// Fallback: basic scheduler (interval only).
	sched, err := cron.ParseSchedule(schedule)
	if err != nil {
		return "", fmt.Errorf("invalid schedule: %w", err)
	}
	sched.Label = name
	cronCommand := command
	cronName := name
	cronCallback := func(runCtx context.Context) error {
		if d != nil && d.SendFn != nil {
			return d.SendFn("cron:"+cronName, cronCommand)
		}
		cmd := exec.CommandContext(runCtx, "bash", "-c", cronCommand)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("cron exec failed: %w\n%s", err, string(out))
		}
		return nil
	}
	if regErr := d.Scheduler.Register(ctx, name, sched, cronCallback); regErr != nil {
		return "", fmt.Errorf("failed to register cron job: %w", regErr)
	}
	return fmt.Sprintf("Cron job %q added (schedule: %s).", name, schedule), nil
}

func cronUpdate(ctx context.Context, d *toolctx.ChronoDeps, jobID, name, schedule, command string, enabled *bool) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId is required for update")
	}
	if d.Service != nil {
		err := d.Service.Update(ctx, jobID, func(j *cron.StoreJob) {
			if name != "" {
				j.Name = name
			}
			if schedule != "" {
				if storeSched, err := cron.ParseSmartSchedule(schedule); err == nil {
					j.Schedule = storeSched
				}
			}
			if command != "" {
				j.Payload.Message = command
				j.Payload.Kind = "agentTurn"
			}
			if enabled != nil {
				j.Enabled = *enabled
			}
		})
		if err != nil {
			return "", fmt.Errorf("update failed: %w", err)
		}
		job := d.Service.GetJob(jobID)
		if job == nil {
			return fmt.Sprintf("Cron job %q updated.", jobID), nil
		}
		schedDesc := formatScheduleDesc(job.Schedule)
		nextInfo := ""
		if job.State.NextRunAtMs > 0 {
			nextInfo = fmt.Sprintf(" Next run: %s.", time.UnixMilli(job.State.NextRunAtMs).Format("2006-01-02 15:04"))
		}
		return fmt.Sprintf("Cron job %q updated (%s, enabled=%v).%s", jobID, schedDesc, job.Enabled, nextInfo), nil
	}
	// Fallback.
	return "", fmt.Errorf("update requires persistent cron service (not available)")
}

func cronRemove(d *toolctx.ChronoDeps, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId is required for remove")
	}
	if d.Service != nil {
		if err := d.Service.Remove(jobID); err != nil {
			return "", fmt.Errorf("remove failed: %w", err)
		}
		return fmt.Sprintf("Cron job %q removed.", jobID), nil
	}
	removed := d.Scheduler.Unregister(jobID)
	if !removed {
		return fmt.Sprintf("Cron job %q not found.", jobID), nil
	}
	return fmt.Sprintf("Cron job %q removed.", jobID), nil
}

func cronRun(ctx context.Context, d *toolctx.ChronoDeps, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId is required for run")
	}
	if d.Service != nil {
		outcome, err := d.Service.Run(ctx, jobID, "force")
		if err != nil {
			return "", fmt.Errorf("run failed: %w", err)
		}
		status := outcome.Status
		if outcome.Error != "" {
			return fmt.Sprintf("Cron job %q run: status=%s, error=%s, duration=%dms", jobID, status, outcome.Error, outcome.DurationMs), nil
		}
		return fmt.Sprintf("Cron job %q run: status=%s, duration=%dms", jobID, status, outcome.DurationMs), nil
	}
	result, err := d.Scheduler.RunNow(ctx, jobID)
	if err != nil {
		return "", fmt.Errorf("run failed: %w", err)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data), nil
}

func cronGet(d *toolctx.ChronoDeps, jobID string) (string, error) {
	if jobID == "" {
		return "", fmt.Errorf("jobId is required for get")
	}
	if d.Service != nil {
		job := d.Service.GetJob(jobID)
		if job == nil {
			return fmt.Sprintf("Cron job %q not found.", jobID), nil
		}
		data, _ := json.MarshalIndent(job, "", "  ")
		return string(data), nil
	}
	st := d.Scheduler.Get(jobID)
	if st == nil {
		return fmt.Sprintf("Cron job %q not found.", jobID), nil
	}
	data, _ := json.MarshalIndent(st, "", "  ")
	return string(data), nil
}

func cronRuns(d *toolctx.ChronoDeps, jobID string, limit int) (string, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if d.RunLog != nil {
		var page cron.RunLogPageResult
		if jobID != "" {
			page = d.RunLog.ReadPage(jobID, cron.RunLogReadOpts{Limit: limit, SortDir: "desc"})
		} else {
			page = d.RunLog.ReadPageAll(cron.RunLogReadOpts{Limit: limit, SortDir: "desc"})
		}
		if len(page.Entries) == 0 {
			if jobID != "" {
				return fmt.Sprintf("No run history for job %q.", jobID), nil
			}
			return "No cron run history.", nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "Run history (%d of %d):\n", len(page.Entries), page.Total)
		for _, e := range page.Entries {
			ts := time.UnixMilli(e.Ts).Format("01-02 15:04")
			dur := ""
			if e.DurationMs > 0 {
				dur = fmt.Sprintf(" %dms", e.DurationMs)
			}
			errStr := ""
			if e.Error != "" {
				errStr = fmt.Sprintf(" err=%s", e.Error)
			}
			summary := ""
			if e.Summary != "" {
				s := e.Summary
				if len(s) > 60 {
					s = s[:57] + "..."
				}
				summary = fmt.Sprintf(" — %s", s)
			}
			sb.WriteString(fmt.Sprintf("\n- [%s] %s: %s%s%s%s", ts, e.JobID, e.Status, dur, errStr, summary))
		}
		return sb.String(), nil
	}
	// Fallback: in-memory run log.
	if d.Scheduler != nil {
		logs := d.Scheduler.Runs(jobID, limit, 0)
		if len(logs) == 0 {
			return "No cron run history.", nil
		}
		data, _ := json.MarshalIndent(logs, "", "  ")
		return string(data), nil
	}
	return "Run history not available.", nil
}

func formatScheduleDesc(s cron.StoreSchedule) string {
	switch s.Kind {
	case "cron":
		if s.Expr != "" {
			return fmt.Sprintf("cron: %s", s.Expr)
		}
		return "cron"
	case "every":
		if s.EveryMs > 0 {
			d := time.Duration(s.EveryMs) * time.Millisecond
			return fmt.Sprintf("every %s", d)
		}
		return "every"
	case "at":
		if s.At != "" {
			return fmt.Sprintf("at: %s", s.At)
		}
		return "one-shot"
	default:
		return s.Kind
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

		result := fmt.Sprintf("Sub-agent spawned.\nSession: %s\nTask: %s", childKey, p.Task)
		if p.ToolPreset != "" {
			result += fmt.Sprintf("\nTool preset: %s", p.ToolPreset)
		}
		return result, nil
	}
}

// --- subagents tool ---
