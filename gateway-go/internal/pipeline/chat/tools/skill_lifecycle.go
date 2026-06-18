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
	RecordSkillValidationCase(context.Context, SkillValidationCaseRequest) (any, error)
	RecordSkillValidationCaseFromSession(context.Context, SkillValidationCaseFromSessionRequest) (any, error)
	BackfillSkillValidationCases(context.Context, SkillValidationBackfillRequest) (any, error)
	RecordSelfCorrectionCandidate(context.Context, SkillSelfCorrectionCandidateRequest) (any, error)
	ReviewSelfCorrectionCandidate(context.Context, SkillSelfCorrectionReviewRequest) (any, error)
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

// SkillEvolutionRequest triggers improvement of one existing skill. Finding is
// an optional review-provided improvement directive; when set, the evolver uses
// it as the basis for the rewrite even without usage data.
type SkillEvolutionRequest struct {
	SkillName string `json:"skillName"`
	Finding   string `json:"finding,omitempty"`
}

// SkillLifecycleStatusRequest queries recent lifecycle decisions, opportunity
// backlog, usage stats, and curator state so future agents can audit what
// happened and detect repeated near-miss skill/evolution candidates.
type SkillLifecycleStatusRequest struct {
	SkillName string `json:"skillName,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// SkillSelfCorrectionCandidateRequest records a deferred self-correction
// candidate for a future coding agent to review in batch. It must not apply the
// change.
type SkillSelfCorrectionCandidateRequest struct {
	ID             string   `json:"id,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	SkillName      string   `json:"skillName,omitempty"`
	SessionKey     string   `json:"sessionKey,omitempty"`
	Title          string   `json:"title,omitempty"`
	Candidate      string   `json:"candidate,omitempty"`
	Evidence       string   `json:"evidence,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	TargetFiles    []string `json:"targetFiles,omitempty"`
	ProposedChange string   `json:"proposedChange,omitempty"`
	Risk           string   `json:"risk,omitempty"`
	Source         string   `json:"source,omitempty"`
}

// SkillSelfCorrectionReviewRequest updates a deferred candidate after batch
// review. The underlying ledger remains append-only.
type SkillSelfCorrectionReviewRequest struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Reviewer   string `json:"reviewer,omitempty"`
	ReviewNote string `json:"reviewNote,omitempty"`
}

// SkillCuratorActionRequest manually updates curator state for agent-created
// skills without touching user-authored skills or deleting files.
type SkillCuratorActionRequest struct {
	Action    string `json:"action"`
	SkillName string `json:"skillName"`
}

// SkillValidationCaseRequest records a held-out invariant for future candidate
// skill validation.
type SkillValidationCaseRequest struct {
	SkillName           string                 `json:"skillName"`
	ID                  string                 `json:"id,omitempty"`
	Description         string                 `json:"description,omitempty"`
	RequiredSubstrings  []string               `json:"requiredSubstrings,omitempty"`
	ForbiddenSubstrings []string               `json:"forbiddenSubstrings,omitempty"`
	RequiredHeadings    []string               `json:"requiredHeadings,omitempty"`
	Replay              SkillReplayCaseRequest `json:"replay,omitempty"`
	Source              string                 `json:"source,omitempty"`
}

// SkillValidationCaseFromSessionRequest records a held-out invariant by
// extracting the replay trace from a stored transcript. The optional Replay
// fields augment the extracted trace when the reviewer knows the precise
// action/observation that made the session fail.
type SkillValidationCaseFromSessionRequest struct {
	SkillName           string                 `json:"skillName"`
	SessionKey          string                 `json:"sessionKey,omitempty"`
	ID                  string                 `json:"id,omitempty"`
	Description         string                 `json:"description,omitempty"`
	RequiredSubstrings  []string               `json:"requiredSubstrings,omitempty"`
	ForbiddenSubstrings []string               `json:"forbiddenSubstrings,omitempty"`
	RequiredHeadings    []string               `json:"requiredHeadings,omitempty"`
	Replay              SkillReplayCaseRequest `json:"replay,omitempty"`
	Source              string                 `json:"source,omitempty"`
}

// SkillValidationBackfillRequest batch-extracts held-out replay traces from
// recent stored transcripts for a specific skill. Weak automatic cases are
// skipped by the tracker instead of polluting the validation corpus.
type SkillValidationBackfillRequest struct {
	SkillName   string                 `json:"skillName"`
	SessionKey  string                 `json:"sessionKey,omitempty"`
	Limit       int                    `json:"limit,omitempty"`
	Description string                 `json:"description,omitempty"`
	Replay      SkillReplayCaseRequest `json:"replay,omitempty"`
	Source      string                 `json:"source,omitempty"`
}

// SkillReplayCaseRequest records a realistic dry-run task and expected action
// choices for held-out skill validation.
type SkillReplayCaseRequest struct {
	Input                 string                       `json:"input,omitempty"`
	Context               []string                     `json:"context,omitempty"`
	RequiredActions       []string                     `json:"requiredActions,omitempty"`
	ForbiddenActions      []string                     `json:"forbiddenActions,omitempty"`
	RequiredObservations  []string                     `json:"requiredObservations,omitempty"`
	ForbiddenObservations []string                     `json:"forbiddenObservations,omitempty"`
	RequiredTools         []string                     `json:"requiredTools,omitempty"`
	ForbiddenTools        []string                     `json:"forbiddenTools,omitempty"`
	ExpectedToolCalls     []SkillReplayToolCallRequest `json:"expectedToolCalls,omitempty"`
	ForbiddenToolCalls    []SkillReplayToolCallRequest `json:"forbiddenToolCalls,omitempty"`
	RequireOrder          bool                         `json:"requireOrder,omitempty"`
}

// SkillReplayToolCallRequest records one expected or forbidden tool invocation
// shape for replay validation.
type SkillReplayToolCallRequest struct {
	Name          string   `json:"name,omitempty"`
	InputIncludes []string `json:"inputIncludes,omitempty"`
	InputExcludes []string `json:"inputExcludes,omitempty"`
	FixtureOutput string   `json:"fixtureOutput,omitempty"`
	FixtureError  bool     `json:"fixtureError,omitempty"`
}

// ToolSkillLifecycle exposes propose/genesis/evolve/status as one agent-facing tool.
func ToolSkillLifecycle(backend SkillLifecycleBackend) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if backend == nil {
			return "", fmt.Errorf("skill_lifecycle backend is not configured")
		}
		var p struct {
			Action string `json:"action"`

			Candidate           string                 `json:"candidate"`
			Evidence            string                 `json:"evidence"`
			Route               string                 `json:"route"`
			Reason              string                 `json:"reason"`
			SessionKey          string                 `json:"sessionKey"`
			DreamSummary        string                 `json:"dreamSummary"`
			SkillName           string                 `json:"skillName"`
			Finding             string                 `json:"finding"`
			Execute             bool                   `json:"execute"`
			Limit               int                    `json:"limit"`
			Scope               string                 `json:"scope"`
			Title               string                 `json:"title"`
			TargetFiles         []string               `json:"targetFiles"`
			ProposedChange      string                 `json:"proposedChange"`
			Risk                string                 `json:"risk"`
			Status              string                 `json:"status"`
			Reviewer            string                 `json:"reviewer"`
			ReviewNote          string                 `json:"reviewNote"`
			ID                  string                 `json:"id"`
			Description         string                 `json:"description"`
			RequiredSubstrings  []string               `json:"requiredSubstrings"`
			ForbiddenSubstrings []string               `json:"forbiddenSubstrings"`
			RequiredHeadings    []string               `json:"requiredHeadings"`
			Replay              SkillReplayCaseRequest `json:"replay"`
			Source              string                 `json:"source"`
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
				Finding:   p.Finding,
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
		case "validation_case":
			result, err = backend.RecordSkillValidationCase(ctx, SkillValidationCaseRequest{
				SkillName:           p.SkillName,
				ID:                  p.ID,
				Description:         p.Description,
				RequiredSubstrings:  p.RequiredSubstrings,
				ForbiddenSubstrings: p.ForbiddenSubstrings,
				RequiredHeadings:    p.RequiredHeadings,
				Replay:              p.Replay,
				Source:              p.Source,
			})
		case "validation_case_from_session":
			result, err = backend.RecordSkillValidationCaseFromSession(ctx, SkillValidationCaseFromSessionRequest{
				SkillName:           p.SkillName,
				SessionKey:          p.SessionKey,
				ID:                  p.ID,
				Description:         p.Description,
				RequiredSubstrings:  p.RequiredSubstrings,
				ForbiddenSubstrings: p.ForbiddenSubstrings,
				RequiredHeadings:    p.RequiredHeadings,
				Replay:              p.Replay,
				Source:              p.Source,
			})
		case "validation_backfill":
			result, err = backend.BackfillSkillValidationCases(ctx, SkillValidationBackfillRequest{
				SkillName:   p.SkillName,
				SessionKey:  p.SessionKey,
				Limit:       p.Limit,
				Description: p.Description,
				Replay:      p.Replay,
				Source:      p.Source,
			})
		case "self_correction":
			result, err = backend.RecordSelfCorrectionCandidate(ctx, SkillSelfCorrectionCandidateRequest{
				ID:             p.ID,
				Scope:          p.Scope,
				SkillName:      p.SkillName,
				SessionKey:     p.SessionKey,
				Title:          p.Title,
				Candidate:      p.Candidate,
				Evidence:       p.Evidence,
				Reason:         p.Reason,
				TargetFiles:    p.TargetFiles,
				ProposedChange: p.ProposedChange,
				Risk:           p.Risk,
				Source:         p.Source,
			})
		case "self_correction_review":
			result, err = backend.ReviewSelfCorrectionCandidate(ctx, SkillSelfCorrectionReviewRequest{
				ID:         p.ID,
				Status:     p.Status,
				Reviewer:   p.Reviewer,
				ReviewNote: p.ReviewNote,
			})
		default:
			return "action은 propose, genesis, evolve, status, self_correction, self_correction_review, validation_case, validation_case_from_session, validation_backfill, pin, unpin, archive, restore 중 하나를 지정하세요.", nil
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
				"description": "Action: propose (record/route a self-evolution proposal), genesis (generate a skill from sessionKey or dreamSummary), evolve (improve an existing skill), status (inspect recent lifecycle logs, opportunity backlog, usage stats, curator state, and pending self-corrections), self_correction (record a deferred correction candidate without applying it), self_correction_review (mark a candidate accepted/rejected/superseded/applied after batch review), validation_case (record held-out assertions for a skill), validation_case_from_session (extract a held-out replay trace from sessionKey), validation_backfill (batch-extract held-out replay traces from stored sessions for skillName), pin/unpin/archive/restore (manual curator state for agent-created skills)",
				"enum":        []string{"propose", "genesis", "evolve", "status", "self_correction", "self_correction_review", "validation_case", "validation_case_from_session", "validation_backfill", "pin", "unpin", "archive", "restore"},
			},
			"candidate": map[string]any{
				"type":        "string",
				"description": "Reusable workflow pattern being proposed. Required for propose unless route=no-op (no-op records 'no reusable pattern', so candidate is optional there)",
			},
			"dreamSummary": map[string]any{
				"type":        "string",
				"description": "Compact dream/summary text to turn into a skill (genesis/propose route=genesis)",
			},
			"evidence": map[string]any{
				"type":        "string",
				"description": "Brief evidence for the proposal: tools used, repeated pitfall, or user request",
			},
			"finding": map[string]any{
				"type":        "string",
				"description": "For evolve: optional review finding or improvement directive to prioritize even when usage stats are sparse",
			},
			"execute": map[string]any{
				"type":        "boolean",
				"description": "For propose: immediately execute the selected route when possible (default false)",
				"default":     false,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "For status: maximum recent lifecycle/self-correction entries to return; for validation_backfill: maximum session keys to scan (default 20, max 50)",
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
				"description": "Session key to use for genesis from transcript context, validation_case_from_session replay extraction, or a single-session validation_backfill",
			},
			"skillName": map[string]any{
				"type":        "string",
				"description": "Existing skill name for evolve/status/validation_backfill, or optional target/related skill for propose",
			},
			"id": map[string]any{
				"type":        "string",
				"description": "For validation_case: stable case id. For self_correction: optional candidate id. For self_correction_review: required candidate id",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "For self_correction: candidate scope, such as skill, code, prompt, docs, ops, config, test, or other",
			},
			"title": map[string]any{
				"type":        "string",
				"description": "For self_correction: short human-readable title for the deferred candidate",
			},
			"targetFiles": map[string]any{
				"type":        "array",
				"description": "For self_correction: repo-relative files or skill paths a future coding agent should inspect",
				"items":       map[string]any{"type": "string"},
			},
			"proposedChange": map[string]any{
				"type":        "string",
				"description": "For self_correction: concrete change idea. Do not apply it in this action",
			},
			"risk": map[string]any{
				"type":        "string",
				"description": "For self_correction: risk, validation need, or rollback concern for the future reviewer",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "For self_correction_review: accepted, rejected, superseded, or applied",
				"enum":        []string{"accepted", "rejected", "superseded", "applied"},
			},
			"reviewer": map[string]any{
				"type":        "string",
				"description": "For self_correction_review: reviewer identity, such as codex or operator",
			},
			"reviewNote": map[string]any{
				"type":        "string",
				"description": "For self_correction_review: why this status was chosen, including tests or PR if applied",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "For validation_case/validation_case_from_session/validation_backfill: what real failure or invariant this held-out case protects",
			},
			"requiredSubstrings": map[string]any{
				"type":        "array",
				"description": "For validation_case: substrings candidate skill bodies must contain",
				"items":       map[string]any{"type": "string"},
			},
			"forbiddenSubstrings": map[string]any{
				"type":        "array",
				"description": "For validation_case: substrings candidate skill bodies must not contain",
				"items":       map[string]any{"type": "string"},
			},
			"requiredHeadings": map[string]any{
				"type":        "array",
				"description": "For validation_case: markdown headings candidate skill bodies must preserve",
				"items":       map[string]any{"type": "string"},
			},
			"source": map[string]any{
				"type":        "string",
				"description": "For validation_case/validation_case_from_session/validation_backfill: source of this held-out case, such as review-finding, session-backfill, or operator",
			},
			"replay": map[string]any{
				"type":        "object",
				"description": "For validation_case: deterministic dry-run replay task and expected action/tool choices. For validation_case_from_session/validation_backfill: optional extra assertions merged with the extracted trace",
				"properties": map[string]any{
					"input": map[string]any{
						"type":        "string",
						"description": "Realistic user task to replay against the candidate skill",
					},
					"context": map[string]any{
						"type":        "array",
						"description": "Relevant state or transcript facts for the replay",
						"items":       map[string]any{"type": "string"},
					},
					"requiredActions": map[string]any{
						"type":        "array",
						"description": "Action phrases the candidate skill must lead the agent to perform",
						"items":       map[string]any{"type": "string"},
					},
					"forbiddenActions": map[string]any{
						"type":        "array",
						"description": "Action phrases the candidate skill must avoid",
						"items":       map[string]any{"type": "string"},
					},
					"requiredObservations": map[string]any{
						"type":        "array",
						"description": "Observation or verification phrases the candidate skill must preserve from fixture outputs",
						"items":       map[string]any{"type": "string"},
					},
					"forbiddenObservations": map[string]any{
						"type":        "array",
						"description": "Observation or conclusion phrases the candidate skill must not introduce",
						"items":       map[string]any{"type": "string"},
					},
					"requiredTools": map[string]any{
						"type":        "array",
						"description": "Tool or command names the candidate skill must preserve for this replay",
						"items":       map[string]any{"type": "string"},
					},
					"forbiddenTools": map[string]any{
						"type":        "array",
						"description": "Tool or command names the candidate skill must not introduce for this replay",
						"items":       map[string]any{"type": "string"},
					},
					"expectedToolCalls": map[string]any{
						"type":        "array",
						"description": "Expected tool-call shapes from a successful trace; each item may require a tool name and important input substrings",
						"items":       skillReplayToolCallSchema(),
					},
					"forbiddenToolCalls": map[string]any{
						"type":        "array",
						"description": "Forbidden tool-call shapes that future candidates must not introduce",
						"items":       skillReplayToolCallSchema(),
					},
					"requireOrder": map[string]any{
						"type":        "boolean",
						"description": "When true, expectedToolCalls must appear in the recorded order",
						"default":     false,
					},
				},
			},
		},
		"required": []string{"action"},
	}
}

func skillReplayToolCallSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Agent tool name, such as exec, read, skills, or skill_lifecycle",
			},
			"inputIncludes": map[string]any{
				"type":        "array",
				"description": "Substrings that must appear in the tool input or command described by the candidate skill",
				"items":       map[string]any{"type": "string"},
			},
			"inputExcludes": map[string]any{
				"type":        "array",
				"description": "Substrings that must not appear in the tool input or command described by the candidate skill",
				"items":       map[string]any{"type": "string"},
			},
			"fixtureOutput": map[string]any{
				"type":        "string",
				"description": "Recorded side-effect-free fixture output for this tool call; not counted as candidate behavior by itself",
			},
			"fixtureError": map[string]any{
				"type":        "boolean",
				"description": "Whether the fixture output represents a tool error",
				"default":     false,
			},
		},
	}
}
