package handlerminiapp

import (
	"context"
	"errors"
	"strings"

	domainprompts "github.com/choiceoh/deneb/gateway-go/internal/domain/prompts"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// PromptDeps wires the native Settings prompt corner to the gateway prompt
// store. The store owns the template registry and the user overrides; handlers
// are a thin CRUD projection.
type PromptDeps struct {
	Store *domainprompts.Store
}

// PromptRow is the lightweight prompt list row shown in Settings.
//
//deneb:wire
type PromptRow struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Editable    bool   `json:"editable"`
	Overridden  bool   `json:"overridden"`
	UpdatedAtMs int64  `json:"updatedAtMs,omitempty"`
}

// PromptDetailOut is the editable prompt document.
//
//deneb:wire
type PromptDetailOut struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Text        string `json:"text"`
	DefaultText string `json:"defaultText"`
	Editable    bool   `json:"editable"`
	Overridden  bool   `json:"overridden"`
	UpdatedAtMs int64  `json:"updatedAtMs,omitempty"`
}

// PromptListResponse is the miniapp.prompts.list payload.
//
//deneb:wire
type PromptListResponse struct {
	Prompts []PromptRow `json:"prompts"`
	Count   int         `json:"count"`
}

func PromptMethods(deps PromptDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.prompts.list":   promptsList(deps),
		"miniapp.prompts.get":    promptsGet(deps),
		"miniapp.prompts.update": promptsUpdate(deps),
		"miniapp.prompts.reset":  promptsReset(deps),
	}
}

func promptsList(deps PromptDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		entries, err := deps.Store.List()
		if err != nil {
			return rpcerr.WrapUnavailable("prompt store unavailable", err).Response(req.ID)
		}
		rows := make([]PromptRow, 0, len(entries))
		for _, entry := range entries {
			rows = append(rows, promptRow(entry))
		}
		return rpcutil.RespondOK(req.ID, PromptListResponse{Prompts: rows, Count: len(rows)})
	}
}

func promptsGet(deps PromptDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		entry, ok, err := deps.Store.Get(id)
		if err != nil {
			return rpcerr.WrapUnavailable("prompt store unavailable", err).Response(req.ID)
		}
		if !ok {
			return rpcerr.NotFound("prompt " + rpcutil.TruncateForError(id)).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, promptDetail(entry))
	}
}

func promptsUpdate(deps PromptDeps) rpcutil.HandlerFunc {
	type params struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if strings.TrimSpace(p.Text) == "" {
			return rpcerr.InvalidRequest("prompt text cannot be empty").Response(req.ID)
		}
		entry, err := deps.Store.Set(id, p.Text)
		if err != nil {
			return promptStoreError(req.ID, id, "prompt update failed", err)
		}
		return rpcutil.RespondOK(req.ID, promptDetail(entry))
	}
}

func promptsReset(deps PromptDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		entry, err := deps.Store.Reset(id)
		if err != nil {
			return promptStoreError(req.ID, id, "prompt reset failed", err)
		}
		return rpcutil.RespondOK(req.ID, promptDetail(entry))
	}
}

func promptStoreError(reqID, id, msg string, err error) *protocol.ResponseFrame {
	switch {
	case errors.Is(err, domainprompts.ErrNotFound):
		return rpcerr.NotFound("prompt " + rpcutil.TruncateForError(id)).Response(reqID)
	case errors.Is(err, domainprompts.ErrReadOnly),
		errors.Is(err, domainprompts.ErrEmpty),
		errors.Is(err, domainprompts.ErrTooLarge):
		return rpcerr.InvalidRequest(err.Error()).Response(reqID)
	default:
		return rpcerr.WrapUnavailable(msg, err).Response(reqID)
	}
}

func promptRow(entry domainprompts.Entry) PromptRow {
	return PromptRow{
		ID:          entry.ID,
		Title:       entry.Title,
		Description: entry.Description,
		Category:    entry.Category,
		Editable:    entry.Editable,
		Overridden:  entry.Overridden,
		UpdatedAtMs: entry.UpdatedAtMs,
	}
}

func promptDetail(entry domainprompts.Entry) PromptDetailOut {
	return PromptDetailOut{
		ID:          entry.ID,
		Title:       entry.Title,
		Description: entry.Description,
		Category:    entry.Category,
		Text:        entry.Text,
		DefaultText: entry.DefaultText,
		Editable:    entry.Editable,
		Overridden:  entry.Overridden,
		UpdatedAtMs: entry.UpdatedAtMs,
	}
}
