package skill

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// GenesisDeps holds dependencies for skills.genesis, skills.evolve, and
// skills.usage RPC methods.
type GenesisDeps struct {
	Genesis *genesis.Service
	Evolver *genesis.Evolver
	Tracker *genesis.Tracker
}

// GenesisMethods returns genesis-related RPC handler methods.
// These are registered separately from the core skills.* methods because
// they have different dependencies (LLM client, tracker, etc.).
func GenesisMethods(deps GenesisDeps) map[string]rpcutil.HandlerFunc {
	methods := make(map[string]rpcutil.HandlerFunc)

	if deps.Genesis != nil {
		methods["skills.genesis"] = skillsGenesis(deps)
	}
	if deps.Evolver != nil {
		methods["skills.evolve"] = skillsEvolve(deps)
	}
	if deps.Tracker != nil {
		methods["skills.usage"] = skillsUsage(deps)
		methods["skills.usage_report"] = skillsUsageReport(deps)
	}

	return methods
}

// skillsGenesis triggers skill extraction from a session or dream summary.
func skillsGenesis(deps GenesisDeps) rpcutil.HandlerFunc {
	type params struct {
		// SessionKey triggers genesis from a completed session.
		SessionKey string `json:"sessionKey,omitempty"`
		// DreamSummary triggers genesis from a dream/compaction summary.
		DreamSummary string `json:"dreamSummary,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}

		if p.SessionKey == "" && p.DreamSummary == "" {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":    false,
				"error": "sessionKey or dreamSummary required",
			})
		}

		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()

		var skill *genesis.GeneratedSkill
		var err error

		if p.DreamSummary != "" {
			skill, err = deps.Genesis.GenerateFromDream(ctx, p.DreamSummary)
		} else {
			// Build a minimal session context from session key.
			sctx := genesis.SessionContext{
				Key: p.SessionKey,
			}
			skill, err = deps.Genesis.Generate(ctx, sctx)
		}

		if err != nil {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
		}
		if skill == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":     true,
				"skip":   true,
				"reason": "no skill-worthy pattern detected",
			})
		}

		if err := deps.Genesis.Persist(skill); err != nil {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":    true,
			"skill": skill,
		})
	}
}

// skillsEvolve triggers improvement of an existing skill.
func skillsEvolve(deps GenesisDeps) rpcutil.HandlerFunc {
	type params struct {
		SkillName string `json:"skillName"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}

		if p.SkillName == "" {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":    false,
				"error": "skillName required",
			})
		}

		ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()

		result, err := deps.Evolver.EvolveSkill(ctx, p.SkillName)
		if err != nil {
			return rpcutil.RespondOK(req.ID, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":     true,
			"result": result,
		})
	}
}

// skillsUsage records a skill usage event.
func skillsUsage(deps GenesisDeps) rpcutil.HandlerFunc {
	type params struct {
		SkillName  string `json:"skillName"`
		SessionKey string `json:"sessionKey"`
		Success    bool   `json:"success"`
		ErrorMsg   string `json:"errorMsg,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.SkillName == "" {
			return nil, rpcerr.MissingParam("skillName")
		}
		err := deps.Tracker.RecordUsage(genesis.UsageRecord{
			SkillName:  p.SkillName,
			SessionKey: p.SessionKey,
			Success:    p.Success,
			ErrorMsg:   p.ErrorMsg,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true}, nil
	})
}

// skillsUsageReport returns usage stats for all tracked skills.
func skillsUsageReport(deps GenesisDeps) rpcutil.HandlerFunc {
	type params struct {
		SkillName string `json:"skillName,omitempty"` // optional: filter to one skill
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.SkillName != "" {
			stats, err := deps.Tracker.GetStats(p.SkillName)
			if err != nil {
				return nil, err
			}
			return map[string]any{"stats": stats}, nil
		}
		all, err := deps.Tracker.ListAllStats()
		if err != nil {
			return nil, err
		}
		return map[string]any{"stats": all}, nil
	})
}
