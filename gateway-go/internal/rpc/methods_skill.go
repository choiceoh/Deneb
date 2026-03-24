package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SkillDeps holds the dependencies for skill RPC methods.
type SkillDeps struct {
	Skills      *skill.Manager
	Broadcaster BroadcastFunc
}

// RegisterSkillMethods registers skills.status, skills.bins, skills.install,
// and skills.update RPC methods.
func RegisterSkillMethods(d *Dispatcher, deps SkillDeps) {
	if deps.Skills == nil {
		return
	}

	d.Register("skills.status", skillsStatus(deps))
	d.Register("skills.bins", skillsBins(deps))
	d.Register("skills.install", skillsInstall(deps))
	d.Register("skills.update", skillsUpdate(deps))
}

func skillsStatus(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		status := deps.Skills.GetStatus(p.AgentID)
		resp, _ := protocol.NewResponseOK(req.ID, status)
		return resp
	}
}

func skillsBins(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		bins := deps.Skills.ListBins()
		if bins == nil {
			bins = make([]string, 0)
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"bins": bins})
		return resp
	}
}

func skillsInstall(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Name      string `json:"name"`
			InstallID string `json:"installId"`
			TimeoutMs int64  `json:"timeoutMs,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Name == "" || p.InstallID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "name and installId are required"))
		}

		result := deps.Skills.Install(p.Name, p.InstallID)

		if deps.Broadcaster != nil {
			deps.Broadcaster("skills.changed", map[string]any{
				"action": "installed",
				"name":   p.Name,
			})
		}

		resp, _ := protocol.NewResponseOK(req.ID, result)
		return resp
	}
}

func skillsUpdate(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SkillKey string            `json:"skillKey"`
			Enabled  *bool             `json:"enabled,omitempty"`
			APIKey   string            `json:"apiKey,omitempty"`
			Env      map[string]string `json:"env,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.SkillKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "skillKey is required"))
		}

		updated, err := deps.Skills.Update(p.SkillKey, skill.SkillPatch{
			Enabled: p.Enabled,
			APIKey:  p.APIKey,
			Env:     p.Env,
		})
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("skills.changed", map[string]any{
				"action":   "updated",
				"skillKey": p.SkillKey,
			})
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":       true,
			"skillKey": p.SkillKey,
			"config":   updated.Config,
		})
		return resp
	}
}
