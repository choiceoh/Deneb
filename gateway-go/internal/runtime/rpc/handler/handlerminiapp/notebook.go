// notebook.go — miniapp.notebook.* RPC surface for the desktop client: read
// (list/get), the create / add_source / remove_source / delete writes the
// notebook pane uses to pin evidence, and set_mode to toggle grounding
// strictness (soft/strict). NotebookLM-style scoped source collections; the
// grounded brief synthesis still lives in the chat/agent path (the `notebook`
// tool), reached by opening a "notebook:<id>" chat session.
package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
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
		"miniapp.notebook.list":          notebookListRPC(deps),
		"miniapp.notebook.get":           notebookGetRPC(deps),
		"miniapp.notebook.create":        notebookCreateRPC(deps),
		"miniapp.notebook.add_source":    notebookAddSourceRPC(deps),
		"miniapp.notebook.delete":        notebookDeleteRPC(deps),
		"miniapp.notebook.remove_source": notebookRemoveSourceRPC(deps),
		"miniapp.notebook.set_mode":      notebookSetModeRPC(deps),
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
	Mode        string              `json:"mode,omitempty"` // "" soft (default) / "strict" — grounding strictness; omitted when soft
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
		return rpcutil.RespondOK(req.ID, notebookListPayload(store))
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

		return rpcutil.RespondOK(req.ID, notebookOutFrom(nb))
	}
}

// notebookCreateRPC creates a new (unanchored) notebook and returns its summary
// so the client can open it and start pinning sources.
func notebookCreateRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
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
		if strings.TrimSpace(p.Name) == "" {
			return rpcerr.MissingParam("name").Response(req.ID)
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}
		nb, err := store.Create(p.Name, p.Description)
		if err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, NotebookSummaryOut{
			ID:          nb.ID,
			Name:        nb.Name,
			Description: nb.Description,
			DealRef:     nb.DealRef,
			SourceCount: len(nb.Sources),
			Updated:     nb.Updated,
		})
	}
}

// notebookAddSourceRPC pins a source — a pasted note (Text) or a wiki page (Ref) —
// to a notebook. kind defaults to "wiki" when only a ref is given, else "note".
// (The ingested kinds — file/url/mail, which the gateway reads server-side — are
// added in a follow-up that wires the source readers into this surface.)
func notebookAddSourceRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID    string `json:"id"`
		Kind  string `json:"kind"`
		Ref   string `json:"ref"`
		Title string `json:"title"`
		Text  string `json:"text"`
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
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		kind := strings.TrimSpace(p.Kind)
		if kind == "" {
			if strings.TrimSpace(p.Ref) != "" {
				kind = notebook.KindWiki
			} else {
				kind = notebook.KindNote
			}
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}
		src, err := store.AddSource(id, notebook.Source{Kind: kind, Ref: p.Ref, Title: p.Title, Text: p.Text})
		if err != nil {
			if errors.Is(err, notebook.ErrNotFound) {
				return rpcerr.NotFound("notebook").Response(req.ID)
			}
			// Validation errors (bad kind, missing text/ref) are the caller's fault.
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, NotebookSourceOut{
			Cite:  src.Cite,
			Kind:  src.Kind,
			Ref:   src.Ref,
			Title: src.Title,
			Text:  truncateNotebookSourceText(src.Text),
		})
	}
}

// notebookDeleteRPC deletes a notebook and returns the updated list so the client
// can refresh its rail without a second round-trip.
func notebookDeleteRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
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
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}
		if err := store.Delete(id); err != nil {
			if errors.Is(err, notebook.ErrNotFound) {
				return rpcerr.NotFound("notebook").Response(req.ID)
			}
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, notebookListPayload(store))
	}
}

// notebookRemoveSourceRPC unpins a source by its cite tag and returns the updated
// notebook so the client can repaint without a second get.
func notebookRemoveSourceRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID   string `json:"id"`
		Cite string `json:"cite"`
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
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if strings.TrimSpace(p.Cite) == "" {
			return rpcerr.MissingParam("cite").Response(req.ID)
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}
		if err := store.RemoveSource(id, p.Cite); err != nil {
			if errors.Is(err, notebook.ErrNotFound) {
				return rpcerr.NotFound("notebook").Response(req.ID)
			}
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		nb, ok := store.Get(id)
		if !ok {
			return rpcerr.NotFound("notebook").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, notebookOutFrom(nb))
	}
}

// notebookSetModeRPC toggles a notebook's grounding strictness (soft/strict) and
// returns the updated notebook so the client repaints its mode control without a
// second get. This is the native-UI analogue of the chat `notebook` tool's
// "mode" action: before this, the only way to switch a notebook into strict
// ("이 자료 위주로만, 없으면 '자료에 없음'") grounding was to ask the agent in chat.
func notebookSetModeRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID   string `json:"id"`
		Mode string `json:"mode"`
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
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}
		if err := store.SetMode(id, p.Mode); err != nil {
			if errors.Is(err, notebook.ErrNotFound) {
				return rpcerr.NotFound("notebook").Response(req.ID)
			}
			// An unrecognized mode value ("use soft or strict") is the caller's fault.
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		nb, ok := store.Get(id)
		if !ok {
			return rpcerr.NotFound("notebook").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, notebookOutFrom(nb))
	}
}

// notebookListPayload builds the list-summaries payload from the store's current
// state — shared by list and the post-delete refresh.
func notebookListPayload(store *notebook.Store) NotebookListOut {
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
	return out
}

// notebookOutFrom builds the full detail payload from a notebook — shared by get
// and the post-remove_source refresh.
func notebookOutFrom(nb *notebook.Notebook) NotebookOut {
	out := NotebookOut{
		ID:          nb.ID,
		Name:        nb.Name,
		Description: nb.Description,
		DealRef:     nb.DealRef,
		Mode:        nb.Mode,
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
	return out
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
