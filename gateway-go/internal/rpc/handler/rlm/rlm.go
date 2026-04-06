// Package rlm provides RPC handlers for RLM context externalization.
//
// Methods expose RLM status, configuration, and wiki-backed project/memory
// queries via the gateway RPC layer.
package rlm

import (
	"context"

	rlmpkg "github.com/choiceoh/deneb/gateway-go/internal/rlm"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for rlm.* RPC methods.
type Deps struct {
	Service *rlmpkg.Service
}

// Methods returns all rlm.* RPC handler methods.
// Returns nil if the RLM service is not configured, preventing registration.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Service == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"rlm.status":          rlmStatus(deps),
		"rlm.config":          rlmConfig(deps),
		"rlm.projects.list":   rlmProjectsList(deps),
		"rlm.projects.search": rlmProjectsSearch(deps),
		"rlm.memory.recall":   rlmMemoryRecall(deps),
	}
}

func rlmStatus(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		return deps.Service.Status(), nil
	})
}

func rlmConfig(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		return deps.Service.Config(), nil
	})
}

func rlmProjectsList(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		projects, err := deps.Service.ListProjects()
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "projects": projects}, nil
	})
}

func rlmProjectsSearch(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			results, err := deps.Service.SearchProjects(ctx, p.Query, p.Limit)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}, nil
			}
			return map[string]any{"ok": true, "results": results}, nil
		})
	}
}

func rlmMemoryRecall(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			results, err := deps.Service.RecallMemory(ctx, p.Query, p.Limit)
			if err != nil {
				return map[string]any{"ok": false, "error": err.Error()}, nil
			}
			return map[string]any{"ok": true, "results": results}, nil
		})
	}
}
