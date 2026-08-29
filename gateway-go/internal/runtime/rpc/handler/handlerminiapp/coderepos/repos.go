// Package coderepos exposes the operator's code-repository allowlist over RPC.
//
// Registration is deliberately a human action: what is listed here is what a
// conversation may be pointed at, and (later) where the agent may create a
// worktree. The store owns validation — including refusing the production
// checkout — so these handlers stay a thin transport over it.
package coderepos

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/coderepo"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// Deps is the handler's complete dependency set.
type CodeReposDeps struct {
	Store *coderepo.Store
}

//deneb:wire
type codeRepoOut struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	AddedAtMs int64  `json:"addedAtMs,omitempty"`
}

func rowsOf(repos []coderepo.Repo) []codeRepoOut {
	out := make([]codeRepoOut, 0, len(repos))
	for _, r := range repos {
		out = append(out, codeRepoOut{ID: r.ID, Name: r.Name, Path: r.Path, AddedAtMs: r.AddedAtMs})
	}
	return out
}

// Methods returns the code-repository allowlist handlers. Returns nil when the
// store is not wired, so a gateway without it simply does not advertise them.
func Methods(deps CodeReposDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}

	type registerParams struct {
		Path string `json:"path"`
		Name string `json:"name,omitempty"`
	}
	type idParams struct {
		ID string `json:"id"`
	}

	list := rpcutil.BindHandler[struct{}](func(struct{}) (any, error) {
		return map[string]any{"repos": rowsOf(deps.Store.List())}, nil
	})

	register := rpcutil.BindHandler[registerParams](func(p registerParams) (any, error) {
		path := strings.TrimSpace(p.Path)
		if path == "" {
			return nil, rpcerr.MissingParam("path")
		}
		repo, err := deps.Store.Register(path, p.Name)
		if err != nil {
			// The store's messages name the reason (not a repo, protected
			// production checkout, …) and are written for the operator.
			return nil, rpcerr.InvalidParams(err)
		}
		return map[string]any{"repo": rowsOf([]coderepo.Repo{repo})[0]}, nil
	})

	unregister := rpcutil.BindHandler[idParams](func(p idParams) (any, error) {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if err := deps.Store.Unregister(id); err != nil {
			return nil, rpcerr.InvalidParams(err)
		}
		// Say what did NOT happen: un-registering revokes eligibility, it does
		// not delete the working tree.
		return map[string]any{"unregistered": true, "deletedFiles": false}, nil
	})

	return map[string]rpcutil.HandlerFunc{
		"miniapp.repos.list":       list,
		"miniapp.repos.register":   register,
		"miniapp.repos.unregister": unregister,
	}
}
