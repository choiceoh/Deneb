package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// SkillLifecycleBackend executes Deneb's closed-loop skill lifecycle.
// The chat tool package owns only the agent-facing JSON surface; the server
// supplies the actual genesis/evolution implementation after those services
// have been initialized.
type SkillLifecycleBackend interface {
	ProposeSkillEvolution(context.Context, SkillEvolutionProposalRequest) (any, error)
	RunSkillGenesis(context.Context, SkillGenesisRequest) (any, error)
	RunSkillEvolution(context.Context, SkillEvolutionRequest) (any, error)
	SkillLifecycleStatus(context.Context, SkillLifecycleStatusRequest) (any, error)
	RunSkillCuratorAction(context.Context, SkillCuratorActionRequest) (any, error)
}

// SkillEvolutionProposalRequest records the agent's routing decision after a
// meaningful workflow. When Execute is true, the backend also runs the chosen
// route if it is executable.
type SkillEvolutionProposalRequest struct {
	Candidate    string `json:"candidate"`
	Evidence     string `json:"evidence,omitempty"`
	Route        string `json:"route"`
	Reason       string `json:"reason,omitempty"`
	SessionKey   string `json:"sessionKey,omitempty"`
	DreamSummary string `json:"dreamSummary,omitempty"`
	SkillName    string `json:"skillName,omitempty"`
	Execute      bool   `json:"execute,omitempty"`
}

// SkillGenesisRequest triggers skill generation from either a live session or
// a compact dream summary.
type SkillGenesisRequest struct {
	SessionKey   string `json:"sessionKey,omitempty"`
	DreamSummary string `json:"dreamSummary,omitempty"`
}

// SkillEvolutionRequest triggers improvement of one existing skill.
type SkillEvolutionRequest struct {
	SkillName string `json:"skillName"`
}

// SkillLifecycleStatusRequest queries recent lifecycle decisions, usage stats,
// and curator state so future agents can audit what happened.
type SkillLifecycleStatusRequest struct {
	SkillName string `json:"skillName,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// SkillCuratorActionRequest manually updates curator state for agent-created
// skills without touching user-authored skills or deleting files.
type SkillCuratorActionRequest struct {
	Action    string `json:"action"`
	SkillName string `json:"skillName"`
}

// ToolSkillLifecycle exposes propose/genesis/evolve/status as one agent-facing tool.
func ToolSkillLifecycle(backend SkillLifecycleBackend) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if backend == nil {
			return "", fmt.Errorf("skill_lifecycle backend is not configured")
		}
		var p struct {
			Action string `json:"action"`

			Candidate    string `json:"candidate"`
			Evidence     string `json:"evidence"`
			Route        string `json:"route"`
			Reason       string `json:"reason"`
			SessionKey   string `json:"sessionKey"`
			DreamSummary string `json:"dreamSummary"`
			SkillName    string `json:"skillName"`
			Execute      bool   `json:"execute"`
			Limit        int    `json:"limit"`
		}
		if err := jsonutil.UnmarshalInto("skill_lifecycle params", input, &p); err != nil {
			return "", err
		}

		var (
			result any
			err    error
		)
		switch p.Action {
		case "propose":
			result, err = backend.ProposeSkillEvolution(ctx, SkillEvolutionProposalRequest{
				Candidate:    p.Candidate,
				Evidence:     p.Evidence,
				Route:        p.Route,
				Reason:       p.Reason,
				SessionKey:   p.SessionKey,
				DreamSummary: p.DreamSummary,
				SkillName:    p.SkillName,
				Execute:      p.Execute,
			})
		case "genesis":
			result, err = backend.RunSkillGenesis(ctx, SkillGenesisRequest{
				SessionKey:   p.SessionKey,
				DreamSummary: p.DreamSummary,
			})
		case "evolve":
			result, err = backend.RunSkillEvolution(ctx, SkillEvolutionRequest{
				SkillName: p.SkillName,
			})
		case "status":
			result, err = backend.SkillLifecycleStatus(ctx, SkillLifecycleStatusRequest{
				SkillName: p.SkillName,
				Limit:     p.Limit,
			})
		case "pin", "unpin", "archive", "restore":
			result, err = backend.RunSkillCuratorAction(ctx, SkillCuratorActionRequest{
				Action:    p.Action,
				SkillName: p.SkillName,
			})
		default:
			return "action은 propose, genesis, evolve, status, pin, unpin, archive, restore 중 하나를 지정하세요.", nil
		}
		if err != nil {
			return "", err
		}
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
}

// SkillLifecycleToolSchema returns the JSON schema for the late-bound
// skill_lifecycle tool.
func SkillLifecycleToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "Action: propose (record/route a self-evolution proposal), genesis (generate a skill from sessionKey or dreamSummary), evolve (improve an existing skill), status (inspect recent lifecycle logs, usage stats, and curator state), pin/unpin/archive/restore (manual curator state for agent-created skills)",
				"enum":        []string{"propose", "genesis", "evolve", "status", "pin", "unpin", "archive", "restore"},
			},
			"candidate": map[string]any{
				"type":        "string",
				"description": "Reusable workflow pattern being proposed (propose action)",
			},
			"dreamSummary": map[string]any{
				"type":        "string",
				"description": "Compact dream/summary text to turn into a skill (genesis/propose route=genesis)",
			},
			"evidence": map[string]any{
				"type":        "string",
				"description": "Brief evidence for the proposal: tools used, repeated pitfall, or user request",
			},
			"execute": map[string]any{
				"type":        "boolean",
				"description": "For propose: immediately execute the selected route when possible (default false)",
				"default":     false,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "For status: maximum recent lifecycle log entries to return (default 20, max 50)",
				"minimum":     1,
				"maximum":     50,
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "Why this route was chosen, or why no-op is correct",
			},
			"route": map[string]any{
				"type":        "string",
				"description": "Proposal route: no-op, genesis, create, or evolve",
				"enum":        []string{"no-op", "genesis", "create", "evolve"},
			},
			"sessionKey": map[string]any{
				"type":        "string",
				"description": "Session key to use for genesis from transcript context",
			},
			"skillName": map[string]any{
				"type":        "string",
				"description": "Existing skill name for evolve, or optional target/related skill for propose",
			},
		},
		"required": []string{"action"},
	}
}
