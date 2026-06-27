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
	"encoding/json"
	"strings"

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

// ProjectLinkedNotebook / ProjectLinkedWorkItem are the minimal item projections
// the linked handler matches on — just an ID plus the item's project-ref fields.
// The store types are mapped to these in method_registry.go so the handler stays
// decoupled from the domain stores (and nil sources simply yield no matches).
type ProjectLinkedNotebook struct {
	ID          string
	DealRef     string
	ProjectRefs []string
}

type ProjectLinkedWorkItem struct {
	ID    string
	RefID string
}

// ProjectDeps wires the project handler's read sources. Wiki is a lazy factory
// (the wiki store is created in the late phase, so the lookup defers to
// per-request); a nil factory skips registration, and a factory that returns an
// error surfaces UNAVAILABLE. Notebooks/WorkItems are optional snapshot providers
// for miniapp.project.linked — a nil provider just contributes no matches of that
// type (mail linkage needs no provider: it is read from the project's graph refs).
type ProjectDeps struct {
	Wiki      func() (ProjectStatusSource, error)
	Notebooks func() []ProjectLinkedNotebook
	WorkItems func() []ProjectLinkedWorkItem
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

// ProjectLinkedOut is the miniapp.project.linked response: the IDs of items
// linked to one project, grouped by type, resolved server-side. Clients filter
// their already-fetched lists by these IDs instead of running a local heuristic.
// Calendar and Todo are always empty for now — those items carry no project
// linkage in the data yet — but are kept in the contract so a client reads every
// section uniformly and they light up when their linkage lands.
//
//deneb:wire
type ProjectLinkedOut struct {
	Mail     []string `json:"mail"`
	Calendar []string `json:"calendar"`
	Todo     []string `json:"todo"`
	Workfeed []string `json:"workfeed"`
	Notebook []string `json:"notebook"`
}

// ProjectMethods returns the miniapp.project.* handler map. Returns nil when no
// wiki factory is wired so method_registry.go can skip registration cleanly.
func ProjectMethods(deps ProjectDeps) map[string]rpcutil.HandlerFunc {
	if deps.Wiki == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.project.digests": projectDigests(deps),
		"miniapp.project.linked":  projectLinked(deps),
	}
}

// projectLinked resolves which items (mail/work-feed/notebook) are linked to one
// project, server-side, from the project's identity (name + path + frozen code +
// graph-resolved owned refs — the same the digest ships). Mail IDs come straight
// from the owned refs (mail analysis pages land there via their Related[] edge);
// notebooks and work-feed items are matched against the identity keys. The client
// passes the project 대표페이지 path and filters its already-fetched lists by the
// returned IDs, so the fragile client-side ref-collection heuristic retires.
func projectLinked(deps ProjectDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p struct {
			Path string `json:"path"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if strings.TrimSpace(p.Path) == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		src, err := deps.Wiki()
		if err != nil {
			return rpcerr.WrapUnavailable("project linked unavailable", err).Response(req.ID)
		}
		statuses, err := src.ProjectStatuses()
		if err != nil {
			return rpcerr.WrapUnavailable("project linked unavailable", err).Response(req.ID)
		}

		// Empty (not error) for an unknown project: the corner just shows no items.
		out := ProjectLinkedOut{Mail: []string{}, Calendar: []string{}, Todo: []string{}, Workfeed: []string{}, Notebook: []string{}}
		wantKey := normalizeMatchKey(p.Path)
		var st *wiki.ProjectStatus
		for i := range statuses {
			if normalizeMatchKey(statuses[i].Path) == wantKey {
				st = &statuses[i]
				break
			}
		}
		if st == nil {
			return rpcutil.RespondOK(req.ID, out)
		}

		keys := projectMatchKeys(st.Name, st.Path, st.Code, st.Refs)
		out.Mail = mailIDsFromRefs(st.Refs)
		if deps.Notebooks != nil {
			for _, nb := range deps.Notebooks() {
				refs := append([]string{nb.DealRef}, nb.ProjectRefs...)
				if itemLinkedToProject(keys, refs...) {
					out.Notebook = append(out.Notebook, nb.ID)
				}
			}
		}
		if deps.WorkItems != nil {
			for _, w := range deps.WorkItems() {
				if itemLinkedToProject(keys, w.RefID) {
					out.Workfeed = append(out.Workfeed, w.ID)
				}
			}
		}
		return rpcutil.RespondOK(req.ID, out)
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
