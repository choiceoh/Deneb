// notebook.go — miniapp.notebook.* RPC surface for the desktop client: read
// (list/get), the create / add_source / remove_source / delete writes the
// notebook pane uses to pin evidence, and set_mode to toggle grounding
// strictness (soft/strict). NotebookLM-style scoped source collections; the
// grounded brief synthesis still lives in the chat/agent path (the `notebook`
// tool), reached by opening a "notebook:<id>" chat session.
package knowledge

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
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
	// ExtractText pulls readable text out of an uploaded document (pdf/docx/
	// spreadsheet/hwp/…) so the desktop client can pin a file to a notebook by
	// PICKING it — no manual path entry. This is the "gateway reads server-side"
	// half that add_source's ref-only file kind was waiting on. Nil when the
	// extractor isn't wired, in which case miniapp.notebook.add_file is skipped
	// cleanly (clients fall back to the note/wiki source kinds).
	ExtractText func(ctx context.Context, data []byte, filename, mimeType string) string
}

// NotebookMethods returns the miniapp.notebook.* handler map. Returns nil if no
// store factory is provided so method_registry can register conditionally.
func NotebookMethods(deps NotebookDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	m := map[string]rpcutil.HandlerFunc{
		"miniapp.notebook.list":          notebookListRPC(deps),
		"miniapp.notebook.get":           notebookGetRPC(deps),
		"miniapp.notebook.create":        notebookCreateRPC(deps),
		"miniapp.notebook.add_source":    notebookAddSourceRPC(deps),
		"miniapp.notebook.delete":        notebookDeleteRPC(deps),
		"miniapp.notebook.remove_source": notebookRemoveSourceRPC(deps),
		"miniapp.notebook.set_mode":      notebookSetModeRPC(deps),
	}
	// File ingestion needs the in-house document extractor wired; skip the method
	// cleanly when it isn't (the client keeps note/wiki and hides the 파일 picker).
	if deps.ExtractText != nil {
		m["miniapp.notebook.add_file"] = notebookAddFileRPC(deps)
	}
	return m
}

// NotebookSummaryOut is one notebook in the list payload. ProjectRefs are the
// canonical project 대표페이지 paths the notebook belongs to (resolved at ingestion
// and stamped on the notebook), so the project corner can link a deal notebook to
// its project by exact ref even when the deal's counterparty name differs.
//
//deneb:wire
type NotebookSummaryOut struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	DealRef     string   `json:"dealRef,omitempty"`
	ProjectRefs []string `json:"projectRefs,omitempty"`
	SourceCount int      `json:"sourceCount"`
	Updated     int64    `json:"updated"`
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
	return minibind.Authenticated(func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, notebookListPayload(store))
	})
}

// notebookGetRPC returns one notebook with its pinned sources, resolved by id or
// deal_ref (a deal has at most one notebook).
func notebookGetRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID      string `json:"id"`
		DealRef string `json:"deal_ref"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
}

// notebookCreateRPC creates a new (unanchored) notebook and returns its summary
// so the client can open it and start pinning sources.
func notebookCreateRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
			ProjectRefs: nb.ProjectRefs,
			SourceCount: len(nb.Sources),
			Updated:     nb.Updated,
		})
	})
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
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
}

// notebookAddFileRPC ingests an UPLOADED document into a notebook: the desktop
// client sends the file bytes (base64) after the user picks a file — no path to
// type — and the gateway extracts the readable text (pdf/docx/spreadsheet/hwp/…)
// and pins it as a kind=file source. This is the server-side reader that
// add_source's ref-only file kind (which validation rejects for lack of text)
// was always meant to have.
func notebookAddFileRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID         string `json:"id"`
		Filename   string `json:"filename"`
		MimeType   string `json:"mimeType"`
		DataBase64 string `json:"dataBase64"`
		Title      string `json:"title"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		id := strings.TrimSpace(p.ID)
		if id == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		// Tolerate a `data:...;base64,` prefix the same way the capture handlers do.
		raw := strings.TrimSpace(p.DataBase64)
		if strings.HasPrefix(raw, "data:") {
			if i := strings.IndexByte(raw, ','); i > 0 {
				raw = raw[i+1:]
			}
		}
		if raw == "" {
			return rpcerr.MissingParam("dataBase64").Response(req.ID)
		}
		data, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(data) == 0 {
			return rpcerr.InvalidRequest("file is not valid base64").Response(req.ID)
		}
		filename := strings.TrimSpace(p.Filename)
		text := strings.TrimSpace(deps.ExtractText(ctx, data, filename, p.MimeType))
		if text == "" {
			return rpcerr.Unavailable("no text could be extracted from the file").Response(req.ID)
		}
		// Title defaults to the filename so the chip reads meaningfully; ref keeps the
		// filename as provenance (the extracted text is the durable grounding copy).
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = filename
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("notebook store unavailable", err).Response(req.ID)
		}
		src, err := store.AddSource(id, notebook.Source{Kind: notebook.KindFile, Ref: filename, Title: title, Text: text})
		if err != nil {
			if errors.Is(err, notebook.ErrNotFound) {
				return rpcerr.NotFound("notebook").Response(req.ID)
			}
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, NotebookSourceOut{
			Cite:  src.Cite,
			Kind:  src.Kind,
			Ref:   src.Ref,
			Title: src.Title,
			Text:  truncateNotebookSourceText(src.Text),
		})
	})
}

// notebookDeleteRPC deletes a notebook and returns the updated list so the client
// can refresh its rail without a second round-trip.
func notebookDeleteRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
}

// notebookRemoveSourceRPC unpins a source by its cite tag and returns the updated
// notebook so the client can repaint without a second get.
func notebookRemoveSourceRPC(deps NotebookDeps) rpcutil.HandlerFunc {
	type params struct {
		ID   string `json:"id"`
		Cite string `json:"cite"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
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
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
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
			ProjectRefs: nb.ProjectRefs,
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
