// notebook.go — miniapp.notebook.* RPC surface: read access to the deal-anchored
// notebook collections (list + get) for the native client. NotebookLM-style
// scoped source collections; the brief synthesis stays in the chat/agent path
// (the `notebook` tool), this surface just exposes the pinned evidence.
package handlerminiapp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// maxNotebookSourceTextChars caps each source's body in the get payload so a
// large notebook stays mobile-friendly; the full text lives in the brief path.
const maxNotebookSourceTextChars = 4000

// NotebookDeps holds a lazy factory for the notebook store, so the gateway boots
// cleanly when the store is unavailable (the handlers then surface UNAVAILABLE
// per call instead of crashing at boot).
type NotebookDeps struct {
	Store func() (*notebook.Store, error)
}

// NotebookMethods returns the miniapp.notebook.* handler map. Returns nil if no
// store factory is provided so method_registry can register conditionally.
func NotebookMethods(deps NotebookDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.notebook.list": notebookListRPC(deps),
		"miniapp.notebook.get":  notebookGetRPC(deps),
	}
}

// NotebookSummaryOut is one notebook in the list payload.
//
//deneb:wire
type NotebookSummaryOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	DealRef     string `json:"dealRef,omitempty"`
	SourceCount int    `json:"sourceCount"`
	Updated     int64  `json:"updated"`
}

// NotebookListOut wraps the notebook summaries for miniapp.notebook.list.
//
//deneb:wire
type NotebookListOut struct {
	Notebooks []NotebookSummaryOut `json:"notebooks"`
}

// NotebookSourceOut is one pinned source in the notebook detail payload.
//
//deneb:wire
type NotebookSourceOut struct {
	Cite  string `json:"cite"`
	Kind  string `json:"kind"`
	Ref   string `json:"ref,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
}

// NotebookOut is the full notebook payload for miniapp.notebook.get.
//
//deneb:wire
type NotebookOut struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	DealRef     string              `json:"dealRef,omitempty"`
	Sources     []NotebookSourceOut `json:"sources"`
	Updated     int64               `json:"updated"`
}

// notebookListRPC returns all notebooks (most-recently-updated first).
func notebookListRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}
		nbs := store.List()
		out := NotebookListOut{Notebooks: make([]NotebookSummaryOut, 0, len(nbs))}
		for _, nb := range nbs {
			out.Notebooks = append(out.Notebooks, NotebookSummaryOut{
				ID:          nb.ID,
				Name:        nb.Name,
				Description: nb.Description,
				DealRef:     nb.DealRef,
				SourceCount: len(nb.Sources),
				Updated:     nb.Updated,
			})
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}

// notebookGetRPC returns one notebook with its pinned sources, resolved by id or
// deal_ref (a deal has at most one notebook).
func notebookGetRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID      string `json:"id"`
		DealRef string `json:"deal_ref"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}

		var (
			nb *notebook.Notebook
			ok bool
		)
		switch {
		case strings.TrimSpace(p.ID) != "":
			nb, ok = store.Get(strings.TrimSpace(p.ID))
		case strings.TrimSpace(p.DealRef) != "":
			nb, ok = store.GetByDealRef(strings.TrimSpace(p.DealRef))
		default:
			return rpcerr.MissingParam("id or deal_ref").Response(req.ID)
		}
		if !ok {
			return rpcerr.NotFound("notebook").Response(req.ID)
		}

		out := NotebookOut{
			ID:          nb.ID,
			Name:        nb.Name,
			Description: nb.Description,
			DealRef:     nb.DealRef,
			Updated:     nb.Updated,
			Sources:     make([]NotebookSourceOut, 0, len(nb.Sources)),
		}
		for _, src := range nb.Sources {
			out.Sources = append(out.Sources, NotebookSourceOut{
				Cite:  src.Cite,
				Kind:  src.Kind,
				Ref:   src.Ref,
				Title: src.Title,
				Text:  truncateNotebookSourceText(src.Text),
			})
		}
		return rpcutil.RespondOK(req.ID, out)
	}
}

// truncateNotebookSourceText caps a source body to a mobile-friendly length on a
// rune boundary.
func truncateNotebookSourceText(s string) string {
	r := []rune(s)
	if len(r) <= maxNotebookSourceTextChars {
		return s
	}
	return string(r[:maxNotebookSourceTextChars]) + "…"
}
