// update_plan.go — Progress tracking tool for multi-step work.
//
// Allows the agent to maintain a visible plan during complex tasks.
// The plan is stored per-session and reported to the user via the delivery
// channel (Telegram, WebSocket). Unlike a narration, the plan is structured
// and survives across continuation runs.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// PlanStep is a single step in the agent's plan.
type PlanStep struct {
	Title  string `json:"title"`
	Status string `json:"status"` // pending, in_progress, completed, failed
	Note   string `json:"note,omitempty"`
}

// PlanState holds the current plan for a session.
type PlanState struct {
	Steps   []PlanStep `json:"steps"`
	Summary string     `json:"summary,omitempty"`
}

// ToolUpdatePlan creates the update_plan tool function.
// The planStore callback persists the plan state per session key.
func ToolUpdatePlan(planStore func(sessionKey string, plan *PlanState)) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Steps   []PlanStep `json:"steps"`
			Summary string     `json:"summary"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("invalid update_plan input: %w", err)
		}

		if len(params.Steps) == 0 {
			return "Error: steps array is required and must not be empty.", nil
		}

		// Validate step statuses.
		for i, step := range params.Steps {
			switch step.Status {
			case "pending", "in_progress", "completed", "failed":
				// Valid.
			default:
				return fmt.Sprintf("Error: step[%d] has invalid status %q. Use pending/in_progress/completed/failed.", i, step.Status), nil
			}
		}

		plan := &PlanState{
			Steps:   params.Steps,
			Summary: params.Summary,
		}

		// Persist the plan.
		sessionKey := toolctx.SessionKeyFromContext(ctx)
		if planStore != nil && sessionKey != "" {
			planStore(sessionKey, plan)
		}

		// Format a compact status display.
		return formatPlanStatus(plan), nil
	}
}

// formatPlanStatus creates a compact plan status string.
func formatPlanStatus(plan *PlanState) string {
	var sb strings.Builder
	completed := 0
	total := len(plan.Steps)

	for _, step := range plan.Steps {
		var icon string
		switch step.Status {
		case "completed":
			icon = "✅"
			completed++
		case "in_progress":
			icon = "🔄"
		case "failed":
			icon = "❌"
		default:
			icon = "⬜"
		}
		sb.WriteString(fmt.Sprintf("%s %s", icon, step.Title))
		if step.Note != "" {
			sb.WriteString(fmt.Sprintf(" — %s", step.Note))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("\nProgress: %d/%d completed", completed, total))
	if plan.Summary != "" {
		sb.WriteString(fmt.Sprintf("\nSummary: %s", plan.Summary))
	}

	return sb.String()
}
