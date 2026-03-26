package autonomous

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// autonomousSessionKey is the fixed session key for autonomous cycles.
// Using a fixed key allows transcript accumulation across cycles for context continuity.
const autonomousSessionKey = "autonomous:cycle"

// buildDecisionPrompt constructs the LLM prompt for a decision cycle.
// The prompt includes all active goals sorted by priority and instructions
// for the agent to pick one goal, take concrete action, and report progress.
func buildDecisionPrompt(goals []Goal) string {
	var b strings.Builder

	b.WriteString("# Autonomous Decision Cycle\n\n")
	b.WriteString("You are running in autonomous mode. Your task is to make progress on the highest-priority active goal.\n\n")

	b.WriteString("## Active Goals\n\n")
	for i, g := range goals {
		b.WriteString(fmt.Sprintf("%d. **[%s] %s** (id: `%s`)\n", i+1, strings.ToUpper(g.Priority), g.Description, g.ID))
		if g.LastNote != "" {
			b.WriteString(fmt.Sprintf("   Last progress: %s\n", g.LastNote))
		}
	}

	b.WriteString("\n## Instructions\n\n")
	b.WriteString("1. Pick the highest-priority goal that you can make concrete progress on right now.\n")
	b.WriteString("2. Use available tools (exec, read, write, web_fetch, etc.) to take real action.\n")
	b.WriteString("3. Focus on ONE goal per cycle. Do not try to work on multiple goals.\n")
	b.WriteString("4. If a goal is impossible or blocked, mark it as \"paused\" with an explanation.\n")
	b.WriteString("5. If a goal is fully achieved, mark it as \"completed\".\n\n")

	b.WriteString("## Required Output\n\n")
	b.WriteString("After completing your work, you MUST end your response with a goal update block in this exact format:\n\n")
	b.WriteString("```goal_update\n")
	b.WriteString("{\"goalUpdates\": [{\"id\": \"GOAL_ID\", \"status\": \"active\", \"note\": \"What you accomplished this cycle\"}]}\n")
	b.WriteString("```\n\n")
	b.WriteString("Valid status values: \"active\" (still in progress), \"completed\" (fully done), \"paused\" (blocked/impossible).\n")
	b.WriteString("The note should be a concise Korean summary of what was accomplished or why the goal was paused.\n")

	return b.String()
}

// goalUpdateBlockRegex matches a fenced ```goal_update ... ``` block.
var goalUpdateBlockRegex = regexp.MustCompile("(?s)```goal_update\\s*\n(.+?)\\s*```")

// goalUpdatePayload is the JSON envelope in a goal_update block.
type goalUpdatePayload struct {
	GoalUpdates []GoalUpdate `json:"goalUpdates"`
}

// parseGoalUpdates extracts GoalUpdate entries from the agent's output.
// Looks for a fenced ```goal_update block with JSON content.
func parseGoalUpdates(output string) []GoalUpdate {
	matches := goalUpdateBlockRegex.FindStringSubmatch(output)
	if len(matches) < 2 {
		return nil
	}

	var payload goalUpdatePayload
	if err := json.Unmarshal([]byte(matches[1]), &payload); err != nil {
		return nil
	}

	// Validate updates.
	valid := make([]GoalUpdate, 0, len(payload.GoalUpdates))
	for _, u := range payload.GoalUpdates {
		if u.ID == "" {
			continue
		}
		switch u.Status {
		case StatusActive, StatusCompleted, StatusPaused:
			// ok
		case "":
			u.Status = StatusActive
		default:
			u.Status = StatusActive
		}
		valid = append(valid, u)
	}
	return valid
}
