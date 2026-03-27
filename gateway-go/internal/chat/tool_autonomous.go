package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/autonomous"
)

func autonomousToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action to perform",
				"enum": []string{
					"status", "goals", "add_goal", "update_goal",
					"remove_goal", "cycle_run", "cycle_stop",
					"enable", "disable", "recent_runs",
				},
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Goal description (for add_goal)",
			},
			"priority": map[string]any{
				"type":        "string",
				"description": "Goal priority (for add_goal, update_goal)",
				"enum":        []string{"high", "medium", "low"},
			},
			"goal_id": map[string]any{
				"type":        "string",
				"description": "Goal ID (for update_goal, remove_goal)",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Goal status (for update_goal)",
				"enum":        []string{"active", "completed", "paused"},
			},
			"filter": map[string]any{
				"type":        "string",
				"description": "Filter goals by status (for goals action)",
				"enum":        []string{"all", "active", "completed", "paused"},
			},
			"count": map[string]any{
				"type":        "number",
				"description": "Number of entries (for recent_runs, default 10)",
			},
		},
		"required": []string{"action"},
	}
}

func toolAutonomous(deps *CoreToolDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action      string `json:"action"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
			GoalID      string `json:"goal_id"`
			Status      string `json:"status"`
			Filter      string `json:"filter"`
			Count       int    `json:"count"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("invalid autonomous params: %w", err)
		}

		svc := deps.AutonomousSvc
		if svc == nil {
			return "Autonomous service not available.", nil
		}

		switch p.Action {
		case "status":
			return autonomousStatus(svc)
		case "goals":
			return autonomousGoals(svc, p.Filter)
		case "add_goal":
			return autonomousAddGoal(svc, p.Description, p.Priority)
		case "update_goal":
			return autonomousUpdateGoal(svc, p.GoalID, p.Priority, p.Status)
		case "remove_goal":
			return autonomousRemoveGoal(svc, p.GoalID)
		case "cycle_run":
			return autonomousCycleRun(svc)
		case "cycle_stop":
			return autonomousCycleStop(svc)
		case "enable":
			return autonomousEnable(svc, true)
		case "disable":
			return autonomousEnable(svc, false)
		case "recent_runs":
			return autonomousRecentRuns(svc, p.Count)
		default:
			return fmt.Sprintf("Unknown autonomous action: %q", p.Action), nil
		}
	}
}

func autonomousStatus(svc *autonomous.Service) (string, error) {
	status := svc.Status()
	data, _ := json.MarshalIndent(status, "", "  ")
	return string(data), nil
}

func autonomousGoals(svc *autonomous.Service, filter string) (string, error) {
	goals, err := svc.Goals().List()
	if err != nil {
		return "", fmt.Errorf("failed to load goals: %w", err)
	}
	if filter != "" && filter != "all" {
		filtered := make([]autonomous.Goal, 0, len(goals))
		for _, g := range goals {
			if g.Status == filter {
				filtered = append(filtered, g)
			}
		}
		goals = filtered
	}
	if len(goals) == 0 {
		return "No goals found.", nil
	}
	data, _ := json.MarshalIndent(goals, "", "  ")
	return string(data), nil
}

func autonomousAddGoal(svc *autonomous.Service, description, priority string) (string, error) {
	if description == "" {
		return "", fmt.Errorf("description is required for add_goal")
	}
	goal, err := svc.AddGoal(description, priority)
	if err != nil {
		return "", fmt.Errorf("failed to add goal: %w", err)
	}
	data, _ := json.MarshalIndent(goal, "", "  ")
	return fmt.Sprintf("Goal added.\n%s", string(data)), nil
}

func autonomousUpdateGoal(svc *autonomous.Service, goalID, priority, status string) (string, error) {
	if goalID == "" {
		return "", fmt.Errorf("goal_id is required for update_goal")
	}
	if priority == "" && status == "" {
		return "", fmt.Errorf("priority or status is required for update_goal")
	}
	if err := svc.Goals().UpdateGoal(goalID, priority, status); err != nil {
		return "", fmt.Errorf("failed to update goal: %w", err)
	}
	return fmt.Sprintf("Goal %q updated.", goalID), nil
}

func autonomousRemoveGoal(svc *autonomous.Service, goalID string) (string, error) {
	if goalID == "" {
		return "", fmt.Errorf("goal_id is required for remove_goal")
	}
	if err := svc.Goals().Remove(goalID); err != nil {
		return "", fmt.Errorf("failed to remove goal: %w", err)
	}
	return fmt.Sprintf("Goal %q removed.", goalID), nil
}

func autonomousCycleRun(svc *autonomous.Service) (string, error) {
	if err := svc.RunCycleAsync(); err != nil {
		return "", fmt.Errorf("failed to start cycle: %w", err)
	}
	return "Autonomous cycle started in background.", nil
}

func autonomousCycleStop(svc *autonomous.Service) (string, error) {
	svc.StopCycle()
	return "Cycle stop requested.", nil
}

func autonomousEnable(svc *autonomous.Service, enabled bool) (string, error) {
	svc.SetEnabled(enabled)
	if enabled {
		return "Autonomous timer enabled.", nil
	}
	return "Autonomous timer disabled.", nil
}

func autonomousRecentRuns(svc *autonomous.Service, count int) (string, error) {
	if count <= 0 {
		count = 10
	}
	runs := svc.RecentRuns(count)
	if len(runs) == 0 {
		return "No recent runs.", nil
	}
	data, _ := json.MarshalIndent(runs, "", "  ")
	return string(data), nil
}
