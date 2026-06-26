// project.go — miniapp.project.* RPC handlers.
//
//   miniapp.project.digests — each active project's latest-progress digest
//                             (its 대표페이지 "## 현재 상태" section + due), newest
//                             first, for the "프로젝트 진행상황" 모아보기 screen.
//
// The digests live ON the project 대표페이지 (프로젝트/<name>.md), written by the
// dream cycle (LLM roll-up) and kept fresh by mail analysis (dated bullets). This
// handler is a thin read of the wiki store — no LLM on the path, so the screen
// loads instantly.

package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProjectStatusSource yields each project's parsed 현재 상태 digest. Satisfied by
// the wiki store (*wiki.Store.ProjectStatuses).
type ProjectStatusSource interface {
	ProjectStatuses() ([]wiki.ProjectStatus, error)
}

// ProjectDeps wires the project handler's read source. Wiki is a lazy factory
// (the wiki store is created in the late phase, so the lookup defers to
// per-request); a nil factory skips registration, and a factory that returns an
// error surfaces UNAVAILABLE.
type ProjectDeps struct {
	Wiki func() (ProjectStatusSource, error)
}

// ProjectDigestRow is one project's latest-progress card for the native client.
// Path is the project 대표페이지's wiki path so a tap opens it; UpdatedAtMs is the
// page's last-updated date in epoch millis (0 when unknown). Code is the page's
// frozen composite project identity (Meta.Code), shipped so clients can match
// items that reference a project by its code, not just its name/path. Marked
// //deneb:wire so the Kotlin/TS types are generated from this one source of truth.
//
//deneb:wire
type ProjectDigestRow struct {
	Project     string   `json:"project"`
	Headline    string   `json:"headline,omitempty"`
	Bullets     []string `json:"bullets,omitempty"`
	Due         string   `json:"due,omitempty"`
	UpdatedAtMs int64    `json:"updatedAtMs,omitempty"`
	Path        string   `json:"path,omitempty"`
	Code        string   `json:"code,omitempty"`
	// Refs are wiki page paths owned by this project (code-shared sub-pages and
	// explicitly-linked pages), resolved server-side from the wiki graph so the
	// client can link items that reference an owned page, not just the 대표페이지.
	Refs []string `json:"refs,omitempty"`
}

// ProjectDigestsOut is the miniapp.project.digests response: every project that
// has a 현재 상태 section, newest first.
//
//deneb:wire
type ProjectDigestsOut struct {
	Digests []ProjectDigestRow `json:"digests"`
}

// ProjectMethods returns the miniapp.project.* handler map. Returns nil when no
// wiki factory is wired so method_registry.go can skip registration cleanly.
func ProjectMethods(deps ProjectDeps) map[string]rpcutil.HandlerFunc {
	if deps.Wiki == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.project.digests": projectDigests(deps),
	}
}

func projectDigests(deps ProjectDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		src, err := deps.Wiki()
		if err != nil {
			return rpcerr.WrapUnavailable("project digests unavailable", err).Response(req.ID)
		}
		statuses, err := src.ProjectStatuses()
		if err != nil {
			return rpcerr.WrapUnavailable("project digests unavailable", err).Response(req.ID)
		}
		rows := make([]ProjectDigestRow, 0, len(statuses))
		for _, st := range statuses {
			rows = append(rows, ProjectDigestRow{
				Project:     st.Name,
				Headline:    st.Summary,
				Bullets:     st.Bullets,
				Due:         st.Due,
				UpdatedAtMs: st.UpdatedMs,
				Path:        st.Path,
				Code:        st.Code,
				Refs:        st.Refs,
			})
		}
		return rpcutil.RespondOK(req.ID, ProjectDigestsOut{Digests: rows})
	}
}
