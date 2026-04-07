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
		"rlm.projects.write":  rlmProjectsWrite(deps),
		"rlm.memory.recall":   rlmMemoryRecall(deps),
		"rlm.memory.store":    rlmMemoryStore(deps),
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

func rlmProjectsWrite(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Path       string   `json:"path,omitempty"`
		Title      string   `json:"title"`
		Content    string   `json:"content,omitempty"`
		Tags       []string `json:"tags,omitempty"`
		Importance float64  `json:"importance,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		result, err := deps.Service.WriteProject(p.Path, p.Title, p.Content, p.Tags, p.Importance)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "path": result.Path, "action": result.Action}, nil
	})
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

func rlmMemoryStore(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Path       string   `json:"path,omitempty"`
		Title      string   `json:"title"`
		Category   string   `json:"category"`
		Content    string   `json:"content,omitempty"`
		Tags       []string `json:"tags,omitempty"`
		Importance float64  `json:"importance,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		result, err := deps.Service.StoreMemory(p.Path, p.Title, p.Category, p.Content, p.Tags, p.Importance)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "path": result.Path, "action": result.Action}, nil
	})
}
