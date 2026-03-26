package chat

import (
	"context"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/autoreply"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
)

// --- cron tool ---

func cronToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Cron action",
				"enum":        []string{"status", "list", "add", "update", "remove", "run", "wake"},
			},
			"jobId": map[string]any{
				"type":        "string",
				"description": "Job ID for update/remove/run actions",
			},
			"job": map[string]any{
				"type":        "object",
				"description": "Job definition for add/update",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "System event text for wake action",
			},
		},
		"required": []string{"action"},
	}
}

func toolCron(cronSched *cron.Scheduler, deps *CoreToolDeps) ToolFunc {
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
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid cron params: %w", err)
		}

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
				if deps != nil && deps.SessionSendFn != nil {
					return deps.SessionSendFn("cron:"+cronName, cronCommand)
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

// --- gateway tool ---

func gatewayToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Gateway action",
				"enum":        []string{"restart", "config.get", "config.schema.lookup", "config.apply", "config.patch", "update.run"},
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Config path for schema.lookup",
			},
			"raw": map[string]any{
				"type":        "string",
				"description": "Raw config JSON for apply/patch",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Reason for restart",
			},
		},
		"required": []string{"action"},
	}
}

func toolGateway(repoDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string         `json:"action"`
			Path   string         `json:"path"`
			Patch  map[string]any `json:"patch"`
			Config map[string]any `json:"config"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid gateway params: %w", err)
		}

		switch p.Action {
		case "config.get":
			snapshot, err := config.LoadConfigFromDefaultPath()
			if err != nil {
				return "Failed to load config: " + err.Error(), nil
			}
			result := map[string]any{
				"path":   snapshot.Path,
				"exists": snapshot.Exists,
				"valid":  snapshot.Valid,
				"hash":   snapshot.Hash,
				"config": snapshot.Config,
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			return string(data), nil

		case "config.schema.lookup":
			node := config.LookupSchema(p.Path)
			if node == nil {
				return fmt.Sprintf("No schema found for path %q.", p.Path), nil
			}
			data, _ := json.MarshalIndent(node, "", "  ")
			return string(data), nil

		case "config.patch":
			if p.Patch == nil {
				return "", fmt.Errorf("patch object is required for config.patch")
			}
			snapshot, err := config.LoadConfigFromDefaultPath()
			if err != nil {
				return "Failed to load config: " + err.Error(), nil
			}
			// Parse current config as map and merge patch.
			var current map[string]any
			if err := json.Unmarshal([]byte(snapshot.Raw), &current); err != nil {
				return "Failed to parse current config: " + err.Error(), nil
			}
			for k, v := range p.Patch {
				current[k] = v
			}
			merged, err := json.MarshalIndent(current, "", "  ")
			if err != nil {
				return "Failed to serialize patched config: " + err.Error(), nil
			}
			cfgPath := config.ResolveConfigPath()
			if err := os.WriteFile(cfgPath, merged, 0644); err != nil {
				return "Failed to write config: " + err.Error(), nil
			}
			return fmt.Sprintf("Config patched successfully. Written to %s", cfgPath), nil

		case "config.apply":
			if p.Config == nil {
				return "", fmt.Errorf("config object is required for config.apply")
			}
			data, err := json.MarshalIndent(p.Config, "", "  ")
			if err != nil {
				return "Failed to serialize config: " + err.Error(), nil
			}
			cfgPath := config.ResolveConfigPath()
			if err := os.WriteFile(cfgPath, data, 0644); err != nil {
				return "Failed to write config: " + err.Error(), nil
			}
			return fmt.Sprintf("Config applied successfully. Written to %s", cfgPath), nil

		case "restart":
			// Send SIGUSR1 to trigger graceful restart.
			proc, err := os.FindProcess(os.Getpid())
			if err != nil {
				return "Failed to find gateway process: " + err.Error(), nil
			}
			if err := proc.Signal(syscall.SIGUSR1); err != nil {
				return "Gateway restart via SIGUSR1 failed: " + err.Error() + ". Use `deneb gateway restart` from the CLI.", nil
			}
			return "Gateway restart signal sent (SIGUSR1). The gateway will restart shortly.", nil

		case "update.run":
			dir := repoDir
			if dir == "" {
				dir, _ = os.Getwd()
			}
			updateCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			// Step 1: git pull
			pullCmd := exec.CommandContext(updateCtx, "git", "pull", "--rebase", "origin", "main")
			pullCmd.Dir = dir
			pullOut, pullErr := pullCmd.CombinedOutput()
			if pullErr != nil {
				return fmt.Sprintf("Update failed at git pull:\n%s\n%s", string(pullOut), pullErr.Error()), nil
			}

			// Step 2: make all
			buildCmd := exec.CommandContext(updateCtx, "make", "all")
			buildCmd.Dir = dir
			buildOut, buildErr := buildCmd.CombinedOutput()
			if buildErr != nil {
				return fmt.Sprintf("Update failed at build:\n%s\n%s", string(buildOut), buildErr.Error()), nil
			}

			// Write sentinel file.
			home, _ := os.UserHomeDir()
			sentinelPath := home + "/.deneb/.update-sentinel"
			sentinel := map[string]any{
				"updatedAt": time.Now().Format(time.RFC3339),
			}
			sentinelData, _ := json.Marshal(sentinel)
			_ = os.WriteFile(sentinelPath, sentinelData, 0644)

			return fmt.Sprintf("Update completed successfully.\nGit pull: %s\nBuild: OK\nRestart the gateway to apply changes.", strings.TrimSpace(string(pullOut))), nil

		default:
			return fmt.Sprintf("Unknown gateway action: %q. Supported: config.get, config.schema.lookup, config.patch, config.apply, restart, update.run", p.Action), nil
		}
	}
}

// --- sessions_list tool ---

func sessionsListToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{
				"type":        "number",
				"description": "Maximum sessions to return",
				"default":     50,
				"minimum":     1,
			},
			"kinds": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": []string{"main", "group", "cron", "hook"}},
				"description": "Filter by session kind",
			},
		},
	}
}

func toolSessionsList(sessions *session.Manager) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		currentKey := SessionKeyFromContext(ctx)

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

func sessionsHistoryToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sessionKey": map[string]any{
				"type":        "string",
				"description": "Session key to fetch history for",
			},
			"limit": map[string]any{
				"type":        "number",
				"description": "Number of messages to return",
				"default":     20,
				"minimum":     1,
			},
		},
		"required": []string{"sessionKey"},
	}
}

func toolSessionsHistory(transcript TranscriptStore) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SessionKey string `json:"sessionKey"`
			Limit      int    `json:"limit"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid sessions_history params: %w", err)
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
			content := msg.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, msg.Role, content)
		}
		return sb.String(), nil
	}
}

// --- sessions_send tool ---

func sessionsSendToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sessionKey": map[string]any{
				"type":        "string",
				"description": "Target session key",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Message to send",
			},
		},
		"required": []string{"message"},
	}
}

func toolSessionsSend(deps *CoreToolDeps) ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SessionKey string `json:"sessionKey"`
			Message    string `json:"message"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid sessions_send params: %w", err)
		}
		if p.Message == "" {
			return "", fmt.Errorf("message is required")
		}

		targetKey := p.SessionKey
		if targetKey == "" {
			targetKey = "main"
		}

		if deps == nil || deps.SessionSendFn == nil {
			return "Cross-session messaging is not available (session send function not wired).", nil
		}

		if err := deps.SessionSendFn(targetKey, p.Message); err != nil {
			return fmt.Sprintf("Failed to send message to session %q: %s", targetKey, err.Error()), nil
		}
		return fmt.Sprintf("Message sent to session %q.", targetKey), nil
	}
}

// --- sessions_spawn tool ---

func sessionsSpawnToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Task description for the sub-agent",
			},
			"label": map[string]any{
				"type":        "string",
				"description": "Human-readable label",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Model override for the sub-agent",
			},
		},
		"required": []string{"task"},
	}
}

func toolSessionsSpawn(deps *CoreToolDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Task  string `json:"task"`
			Label string `json:"label"`
			Model string `json:"model"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid sessions_spawn params: %w", err)
		}
		if p.Task == "" {
			return "", fmt.Errorf("task is required")
		}

		if deps == nil || deps.Sessions == nil || deps.SessionSendFn == nil {
			return "Sub-agent spawning is not available (session dependencies not wired).", nil
		}

		// Create a unique session key for the sub-agent.
		parentKey := SessionKeyFromContext(ctx)
		label := p.Label
		if label == "" {
			label = "subagent"
		}
		childKey := fmt.Sprintf("%s:%s:%d", parentKey, label, time.Now().UnixMilli())

		// Create the child session.
		childSession := deps.Sessions.Create(childKey, session.KindDirect)
		if p.Model != "" {
			childSession.Model = p.Model
		}
		childSession.SpawnedBy = parentKey
		deps.Sessions.Set(childSession)

		// Send the task message to the child session.
		if err := deps.SessionSendFn(childKey, p.Task); err != nil {
			return fmt.Sprintf("Sub-agent session %q created but failed to send task: %s", childKey, err.Error()), nil
		}

		return fmt.Sprintf("Sub-agent spawned.\nSession: %s\nTask: %s", childKey, p.Task), nil
	}
}

// --- subagents tool ---

func subagentsToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Sub-agent management action",
				"enum":        []string{"list", "kill", "steer"},
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Target sub-agent ID or label",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Steering message for steer action",
			},
		},
	}
}

func toolSubagents(deps *CoreToolDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action  string `json:"action"`
			Target  string `json:"target"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid subagents params: %w", err)
		}
		if p.Action == "" {
			p.Action = "list"
		}

		if deps == nil || deps.Sessions == nil {
			return "Sub-agent management not available (session dependencies not wired).", nil
		}

		parentKey := SessionKeyFromContext(ctx)

		// Gather children: sessions where SpawnedBy == parentKey.
		allSessions := deps.Sessions.List()
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
			return subagentsKill(deps, children, p.Target)
		case "steer":
			return subagentsSteer(deps, children, p.Target, p.Message)
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
			parts = append(parts, autoreply.FormatDurationCompact(*c.RuntimeMs))
		} else if c.Status == session.StatusRunning && c.StartedAt != nil {
			elapsed := time.Now().UnixMilli() - *c.StartedAt
			parts = append(parts, autoreply.FormatDurationCompact(elapsed))
		}
		// Tokens.
		if c.TotalTokens != nil && *c.TotalTokens > 0 {
			parts = append(parts, fmt.Sprintf("%dtok", *c.TotalTokens))
		}
		// Model.
		if c.Model != "" {
			parts = append(parts, fmt.Sprintf("model=%s", c.Model))
		}

		fmt.Fprintf(&sb, "  %d. [%s] %s", i+1, status, autoreply.TruncateLine(label, 60))
		if len(parts) > 0 {
			fmt.Fprintf(&sb, " (%s)", strings.Join(parts, ", "))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// subagentsKill kills one or all child sessions.
func subagentsKill(deps *CoreToolDeps, children []*session.Session, target string) (string, error) {
	if target == "" {
		return "Target is required. Use a sub-agent index, label, session key, or \"all\".", nil
	}

	if strings.ToLower(target) == "all" {
		killed := 0
		for _, c := range children {
			if c.Status == session.StatusRunning {
				killSession(deps.Sessions, c)
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
	killSession(deps.Sessions, child)
	return fmt.Sprintf("Killed sub-agent: %s", child.Key), nil
}

// subagentsSteer sends a steering message to a running child session.
func subagentsSteer(deps *CoreToolDeps, children []*session.Session, target, message string) (string, error) {
	if deps.SessionSendFn == nil {
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

	if err := deps.SessionSendFn(child.Key, message); err != nil {
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
	sessions.Set(s)
}

// resolveChildTarget finds a child by 1-based index, exact key, label, or key prefix.
func resolveChildTarget(children []*session.Session, target string) (*session.Session, string) {
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

func sessionStatusToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sessionKey": map[string]any{
				"type":        "string",
				"description": "Session key (defaults to current)",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Set model override for the session",
			},
		},
	}
}

func toolSessionStatus(sessions *session.Manager) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		sessionKey := SessionKeyFromContext(ctx)
		if sessionKey == "" {
			sessionKey = "(unknown)"
		}

		// Allow querying a specific session.
		var p struct {
			SessionKey string `json:"sessionKey"`
			Model      string `json:"model"`
		}
		if len(input) > 0 {
			_ = json.Unmarshal(input, &p)
		}
		if p.SessionKey != "" {
			sessionKey = p.SessionKey
		}

		now := time.Now()
		var sb strings.Builder
		fmt.Fprintf(&sb, "📊 Session Status\n")
		fmt.Fprintf(&sb, "Session: %s\n", sessionKey)
		fmt.Fprintf(&sb, "Time: %s\n", now.Format("2006-01-02 15:04:05"))
		fmt.Fprintf(&sb, "Gateway: Go\n")

		if sessions != nil {
			s := sessions.Get(sessionKey)
			if s != nil {
				fmt.Fprintf(&sb, "Kind: %s\n", s.Kind)
				fmt.Fprintf(&sb, "Status: %s\n", s.Status)
				if s.Model != "" {
					fmt.Fprintf(&sb, "Model: %s\n", s.Model)
				}
				if s.Channel != "" {
					fmt.Fprintf(&sb, "Channel: %s\n", s.Channel)
				}
				if s.InputTokens != nil {
					fmt.Fprintf(&sb, "Input tokens: %d\n", *s.InputTokens)
				}
				if s.OutputTokens != nil {
					fmt.Fprintf(&sb, "Output tokens: %d\n", *s.OutputTokens)
				}
				if s.RuntimeMs != nil {
					fmt.Fprintf(&sb, "Runtime: %dms\n", *s.RuntimeMs)
				}
				if s.SpawnedBy != "" {
					fmt.Fprintf(&sb, "Spawned by: %s\n", s.SpawnedBy)
				}
			} else {
				fmt.Fprintf(&sb, "Status: no session data found\n")
			}

			// Apply model override if requested.
			if p.Model != "" && s != nil {
				s.Model = p.Model
				sessions.Set(s)
				fmt.Fprintf(&sb, "\nModel override applied: %s\n", p.Model)
			}

			// Show total session count.
			fmt.Fprintf(&sb, "\nTotal sessions: %d\n", sessions.Count())
		} else {
			fmt.Fprintf(&sb, "Status: running\n")
		}

		return sb.String(), nil
	}
}

// --- image tool ---

func imageToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What to analyze in the image(s)",
			},
			"image": map[string]any{
				"type":        "string",
				"description": "Single image path or URL",
			},
			"images": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Multiple image paths or URLs (up to 20)",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Vision model to use",
			},
		},
	}
}

func toolImage(client *llm.Client) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Prompt string   `json:"prompt"`
			Image  string   `json:"image"`
			Images []string `json:"images"`
			Model  string   `json:"model"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid image params: %w", err)
		}

		// Collect all image paths/URLs.
		var imagePaths []string
		if p.Image != "" {
			imagePaths = append(imagePaths, p.Image)
		}
		imagePaths = append(imagePaths, p.Images...)
		if len(imagePaths) == 0 {
			return "No images provided. Use 'image' for a single path/URL or 'images' for multiple.", nil
		}
		if len(imagePaths) > 20 {
			return "Too many images (max 20).", nil
		}

		if client == nil {
			return fmt.Sprintf("Vision model not available. %d image(s) provided but no LLM client configured. Images sent in the user's message are already visible to you.", len(imagePaths)), nil
		}

		prompt := p.Prompt
		if prompt == "" {
			prompt = "Describe what you see in the image(s) in detail."
		}

		// Build content blocks with images + text prompt.
		var blocks []llm.ContentBlock
		for _, imgPath := range imagePaths {
			block, err := loadImageBlock(ctx, imgPath)
			if err != nil {
				return fmt.Sprintf("Failed to load image %q: %s", imgPath, err.Error()), nil
			}
			blocks = append(blocks, block)
		}
		blocks = append(blocks, llm.ContentBlock{Type: "text", Text: prompt})

		model := p.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}

		// Call LLM with vision.
		visionCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		events, err := client.StreamChat(visionCtx, llm.ChatRequest{
			Model:     model,
			Messages:  []llm.Message{llm.NewBlockMessage("user", blocks)},
			MaxTokens: 4096,
			Stream:    true,
		})
		if err != nil {
			return fmt.Sprintf("Vision model call failed: %s", err.Error()), nil
		}

		// Collect streaming text response.
		var result strings.Builder
		for ev := range events {
			if ev.Type == "content_block_delta" {
				var delta struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
					result.WriteString(delta.Delta.Text)
				}
			}
		}

		if result.Len() == 0 {
			return "Vision model returned no response.", nil
		}
		return result.String(), nil
	}
}

// loadImageBlock loads an image from a file path or URL and returns an LLM content block.
func loadImageBlock(ctx context.Context, path string) (llm.ContentBlock, error) {
	var data []byte
	var mimeType string

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		// URL: use OpenAI-style image_url block.
		return llm.ContentBlock{
			Type:     "image",
			ImageURL: &llm.ImageURL{URL: path, Detail: "auto"},
		}, nil
	}

	// Local file: read and base64-encode.
	var err error
	data, err = os.ReadFile(path)
	if err != nil {
		return llm.ContentBlock{}, fmt.Errorf("read image file: %w", err)
	}

	// Detect MIME type from magic bytes.
	mimeType = http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/png" // fallback
	}

	encoded := base64Encode(data)
	return llm.ContentBlock{
		Type: "image",
		Source: &llm.ImageSource{
			Type:      "base64",
			MediaType: mimeType,
			Data:      encoded,
		},
	}, nil
}

// base64Encode encodes data to standard base64.
func base64Encode(data []byte) string {
	return b64.StdEncoding.EncodeToString(data)
}

// --- nodes tool ---

func nodesToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Node action",
				"enum":        []string{"status", "describe", "notify", "camera_snap", "location_get", "run", "invoke"},
			},
			"node": map[string]any{
				"type":        "string",
				"description": "Node ID or name",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "Notification title (for notify action)",
			},
			"body": map[string]any{
				"type":        "string",
				"description": "Notification body (for notify action)",
			},
			"command": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Command to run (for run action)",
			},
		},
		"required": []string{"action"},
	}
}

func toolNodes() ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid nodes params: %w", err)
		}

		switch p.Action {
		case "status":
			return "No paired nodes found.", nil
		case "describe":
			return "No nodes available to describe.", nil
		case "notify":
			return "Node notification requires a paired device. No nodes connected.", nil
		default:
			return fmt.Sprintf("Nodes action %q requires a paired device. No nodes connected.", p.Action), nil
		}
	}
}
