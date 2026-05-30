package handlerminiapp

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ModelOption is one selectable model shown in the Mini App settings view.
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
}

// ModelSection groups selectable models by role/provider.
type ModelSection struct {
	Title  string        `json:"title"`
	Models []ModelOption `json:"models"`
}

// ModelAddResult is returned after a direct endpoint/model pair is stored.
type ModelAddResult struct {
	OK       bool   `json:"ok"`
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	Added    bool   `json:"added"`
}

// ModelDeleteResult is returned after a custom model entry is removed.
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
type RoleModel struct {
	Role  string `json:"role"`
	Model string `json:"model"`
}

// ModelDeps holds the lazy model operations exposed to the Mini App.
type ModelDeps struct {
	CurrentModel func() string
	RoleModels   func() []RoleModel
	ListModels   func(context.Context) ([]ModelSection, error)
	SetModel     func(ctx context.Context, role, id string) (string, error)
	AddModel     func(context.Context, string, string) (ModelAddResult, error)
	DeleteModel  func(ctx context.Context, id string) (ModelDeleteResult, error)
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
		if telegram.InitDataFromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.models.list requires initData context").Response(req.ID)
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
		return rpcutil.RespondOK(req.ID, map[string]any{
			"current":  current,
			"roles":    roles,
			"sections": sections,
		})
	}
}

func modelsSet(deps ModelDeps) rpcutil.HandlerFunc {
	type params struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if telegram.InitDataFromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.models.set requires initData context").Response(req.ID)
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
		if telegram.InitDataFromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.models.add_custom requires initData context").Response(req.ID)
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
		if telegram.InitDataFromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.models.delete_custom requires initData context").Response(req.ID)
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
