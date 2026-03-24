package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// AgentsDeps holds the dependencies for agents CRUD RPC methods.
type AgentsDeps struct {
	Agents      *agent.Store
	Broadcaster BroadcastFunc
}

// RegisterAgentsMethods registers all agents.* RPC methods.
func RegisterAgentsMethods(d *Dispatcher, deps AgentsDeps) {
	if deps.Agents == nil {
		return
	}

	d.Register("agents.list", agentsList(deps))
	d.Register("agents.create", agentsCreate(deps))
	d.Register("agents.update", agentsUpdate(deps))
	d.Register("agents.delete", agentsDelete(deps))
	d.Register("agents.files.list", agentsFilesList(deps))
	d.Register("agents.files.get", agentsFilesGet(deps))
	d.Register("agents.files.set", agentsFilesSet(deps))
}

func agentsList(deps AgentsDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		agents := deps.Agents.List()
		if agents == nil {
			agents = make([]*agent.Agent, 0)
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"agents": agents})
		return resp
	}
}

func agentsCreate(deps AgentsDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID      string            `json:"agentId,omitempty"`
			Name         string            `json:"name,omitempty"`
			Description  string            `json:"description,omitempty"`
			Model        string            `json:"model,omitempty"`
			SystemPrompt string            `json:"systemPrompt,omitempty"`
			Tools        []string          `json:"tools,omitempty"`
			Metadata     map[string]string `json:"metadata,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}

		created := deps.Agents.Create(agent.CreateParams{
			AgentID:      p.AgentID,
			Name:         p.Name,
			Description:  p.Description,
			Model:        p.Model,
			SystemPrompt: p.SystemPrompt,
			Tools:        p.Tools,
			Metadata:     p.Metadata,
		})

		if deps.Broadcaster != nil {
			deps.Broadcaster("agents.changed", map[string]any{
				"action":  "created",
				"agentId": created.AgentID,
			})
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"agent": created})
		return resp
	}
}

func agentsUpdate(deps AgentsDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string         `json:"agentId"`
			Patch   map[string]any `json:"patch"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.AgentID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "agentId is required"))
		}

		updated, err := deps.Agents.Update(p.AgentID, p.Patch)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("agents.changed", map[string]any{
				"action":  "updated",
				"agentId": p.AgentID,
			})
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"agent": updated})
		return resp
	}
}

func agentsDelete(deps AgentsDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.AgentID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "agentId is required"))
		}

		removed := deps.Agents.Delete(p.AgentID)
		if !removed {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "agent not found"))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("agents.changed", map[string]any{
				"action":  "deleted",
				"agentId": p.AgentID,
			})
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"removed": true})
		return resp
	}
}

func agentsFilesList(deps AgentsDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.AgentID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "agentId is required"))
		}

		files, err := deps.Agents.ListFiles(p.AgentID)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}
		if files == nil {
			files = make([]*agent.FileEntry, 0)
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{"files": files})
		return resp
	}
}

func agentsFilesGet(deps AgentsDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId"`
			Name    string `json:"name"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.AgentID == "" || p.Name == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "agentId and name are required"))
		}

		file, err := deps.Agents.GetFile(p.AgentID, p.Name)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp, _ := protocol.NewResponseOK(req.ID, file)
		return resp
	}
}

func agentsFilesSet(deps AgentsDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID       string `json:"agentId"`
			Name          string `json:"name"`
			ContentBase64 string `json:"contentBase64,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params"))
		}
		if p.AgentID == "" || p.Name == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "agentId and name are required"))
		}

		file, err := deps.Agents.SetFile(p.AgentID, p.Name, p.ContentBase64)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp, _ := protocol.NewResponseOK(req.ID, file)
		return resp
	}
}
