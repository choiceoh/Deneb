package handlerminiapp

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ModelOption is one selectable model shown in the Mini App settings view.
//
//deneb:wire
type ModelOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider,omitempty"`
	Display  string `json:"display,omitempty"`
	Health   string `json:"health,omitempty"`
	Current  bool   `json:"current"`
	// Custom marks a user-added model (provider custom/custom-N) that the
	// picker may delete; built-in/role models leave this false.
	Custom bool `json:"custom,omitempty"`
	// Deletable marks a model the picker may remove. True for user-added custom
	// models AND cloud-catalog provider models (openrouter/zai/kimi/mimo-plan/…);
	// local vLLM/localai models are role-critical and stay false. The delete path
	// removes custom models from config and adds built-in cloud models to
	// models.hiddenModels (a soft hide that survives the built-in re-merge).
	Deletable bool `json:"deletable,omitempty"`
	// Unhealthy is the model-health circuit breaker state (consecutive
	// failures; initial attempts are being skipped in favor of fallback).
	// Distinct from Health, which is endpoint reachability.
	Unhealthy bool `json:"unhealthy,omitempty"`
	// Note is a server-rendered Korean stat line from the model tuner's
	// latest scorecard window (runs, p95, cache hit, fallback/stall counts,
	// calibration probe, tuned output floor). Empty when no data yet.
	Note string `json:"note,omitempty"`
}

// ModelSection groups selectable models by role/provider.
//
//deneb:wire
type ModelSection struct {
	Title  string        `json:"title"`
	Models []ModelOption `json:"models"`
}

// ModelAddResult is returned after a direct endpoint/model pair is stored.
//
//deneb:wire
type ModelAddResult struct {
	OK       bool   `json:"ok"`
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	Added    bool   `json:"added"`
}

// ModelDeleteResult is returned after a custom model entry is removed.
//
//deneb:wire
type ModelDeleteResult struct {
	OK      bool   `json:"ok"`
	ID      string `json:"id"`
	Removed bool   `json:"removed"`
	// ClearedRoles names the roles (main/lightweight/fallback) that were reset
	// to the default because they had been bound to the deleted model.
	ClearedRoles []string `json:"clearedRoles,omitempty"`
	Current      string   `json:"current"`
}

// RoleModel reports the model bound to a registry role (main/lightweight/
// fallback), for the per-role model picker.
//
//deneb:wire
type RoleModel struct {
	Role  string `json:"role"`
	Model string `json:"model"`
}

// ModelsListResult is the miniapp.models.list response: the active model, the
// per-role bindings, and the grouped selectable sections. Promoted from an
// ad-hoc map[string]any wrapper so the response shape is a single source of
// truth (its element types RoleModel/ModelSection are already wire types) and
// the native client gets a generated Kotlin type. Wire JSON is unchanged.
//
//deneb:wire
type ModelsListResult struct {
	Current  string         `json:"current"`
	Roles    []RoleModel    `json:"roles"`
	Sections []ModelSection `json:"sections"`
	// Advisories are the model tuner's open recommendations as Korean
	// display lines ("provider/model: message"). Empty when all is well.
	Advisories []string `json:"advisories,omitempty"`
	// MainHasVision reports whether the main model accepts image input. The
	// native picker hides the opt-in 비전 role row when true — a multimodal main
	// model makes a separate vision model redundant (images route to main
	// directly). Mirrors the gateway's own routing: false exactly when main is
	// marked vision:false and image turns would otherwise be stripped
	// (run_prepare.go). Defaults false on an older gateway, so the row shows.
	MainHasVision bool `json:"mainHasVision"`
}

// ModelDeps holds the lazy model operations exposed to the Mini App.
type ModelDeps struct {
	CurrentModel func() string
	RoleModels   func() []RoleModel
	ListModels   func(context.Context) ([]ModelSection, error)
	SetModel     func(ctx context.Context, role, id string) (string, error)
	AddModel     func(context.Context, string, string) (ModelAddResult, error)
	DeleteModel  func(ctx context.Context, id string) (ModelDeleteResult, error)
	// Advisories returns the model tuner's open recommendations as display
	// lines. Optional: nil omits the field.
	Advisories func() []string
	// MainHasVision reports whether the main model accepts image input, so the
	// picker can hide the opt-in vision role when it's redundant. nil → false.
	MainHasVision func() bool
}

// ModelMethods returns Mini App model quick-change RPC handlers.
func ModelMethods(deps ModelDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"miniapp.models.add_custom":    modelsAddCustom(deps),
		"miniapp.models.delete_custom": modelsDeleteCustom(deps),
		"miniapp.models.list":          modelsList(deps),
		"miniapp.models.set":           modelsSet(deps),
	}
}

func modelsList(deps ModelDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.models.list requires client identity context").Response(req.ID)
		}
		if deps.ListModels == nil {
			return rpcerr.Unavailable("model list is unavailable").Response(req.ID)
		}
		sections, err := deps.ListModels(ctx)
		if err != nil {
			return rpcerr.WrapDependencyFailed("list models", err).Response(req.ID)
		}
		current := ""
		if deps.CurrentModel != nil {
			current = deps.CurrentModel()
		}
		roles := []RoleModel{}
		if deps.RoleModels != nil {
			roles = deps.RoleModels()
		}
		var advisories []string
		if deps.Advisories != nil {
			advisories = deps.Advisories()
		}
		return rpcutil.RespondOK(req.ID, ModelsListResult{
			Current:       current,
			Roles:         roles,
			Sections:      sections,
			Advisories:    advisories,
			MainHasVision: deps.MainHasVision != nil && deps.MainHasVision(),
		})
	}
}

func modelsSet(deps ModelDeps) rpcutil.HandlerFunc {
	type params struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.models.set requires client identity context").Response(req.ID)
		}
		return rpcutil.BindCtx[params](ctx, req, func(ctx context.Context, p params) (any, error) {
			id := strings.TrimSpace(p.ID)
			if id == "" {
				return nil, rpcerr.MissingParam("id")
			}
			role := strings.TrimSpace(p.Role)
			if role == "" {
				role = "main"
			}
			if deps.SetModel == nil {
				return nil, rpcerr.Unavailable("model switch is unavailable")
			}
			current, err := deps.SetModel(ctx, role, id)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"ok":      true,
				"role":    role,
				"current": current,
			}, nil
		})
	}
}

func modelsAddCustom(deps ModelDeps) rpcutil.HandlerFunc {
	type params struct {
		Endpoint string `json:"endpoint"`
		Model    string `json:"model"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.models.add_custom requires client identity context").Response(req.ID)
		}
		return rpcutil.BindCtx[params](ctx, req, func(ctx context.Context, p params) (any, error) {
			endpoint := strings.TrimSpace(p.Endpoint)
			if endpoint == "" {
				return nil, rpcerr.MissingParam("endpoint")
			}
			model := strings.TrimSpace(p.Model)
			if model == "" {
				return nil, rpcerr.MissingParam("model")
			}
			if deps.AddModel == nil {
				return nil, rpcerr.Unavailable("custom model add is unavailable")
			}
			result, err := deps.AddModel(ctx, endpoint, model)
			if err != nil {
				return nil, err
			}
			result.OK = true
			return result, nil
		})
	}
}

func modelsDeleteCustom(deps ModelDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.models.delete_custom requires client identity context").Response(req.ID)
		}
		return rpcutil.BindCtx[params](ctx, req, func(ctx context.Context, p params) (any, error) {
			id := strings.TrimSpace(p.ID)
			if id == "" {
				return nil, rpcerr.MissingParam("id")
			}
			if deps.DeleteModel == nil {
				return nil, rpcerr.Unavailable("custom model delete is unavailable")
			}
			result, err := deps.DeleteModel(ctx, id)
			if err != nil {
				return nil, err
			}
			result.OK = true
			return result, nil
		})
	}
}
