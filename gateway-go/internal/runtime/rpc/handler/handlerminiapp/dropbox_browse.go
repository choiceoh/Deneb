// dropbox_browse.go — miniapp.dropbox.* file-browser RPCs (read/share/upload).
//
//	miniapp.dropbox.list    — list a folder's entries
//	miniapp.dropbox.search  — search the account by query
//	miniapp.dropbox.share   — create (or fetch) a shared link for a path
//	miniapp.dropbox.upload  — upload device bytes to a destination path
//
// These are pure metadata/byte operations over the Dropbox client, so they live
// here (handlerminiapp) and import platform/dropbox directly — the calendar.go
// pattern. "Analyze a file" is NOT here: it runs a full agent turn (the agent's
// own dropbox tool does the extraction) and lives in the chat bridge, so this
// package never imports pipeline/chat/tools. The connect wizard (status/begin/
// complete) stays in dropbox.go.

package handlerminiapp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropbox"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	defaultDropboxListLimit = 200
	defaultDropboxSearchMax = 50
)

// DropboxEntryOut is one Dropbox file/folder row. Marked for Kotlin codegen so
// the native client shares this exact shape (Size is bytes; the client formats
// it). Tag is "file" or "folder".
//
//deneb:wire
type DropboxEntryOut struct {
	Tag            string `json:"tag"`
	Name           string `json:"name"`
	PathDisplay    string `json:"pathDisplay"`
	PathLower      string `json:"pathLower"`
	ID             string `json:"id,omitempty"`
	Size           int64  `json:"size,omitempty"`
	ServerModified string `json:"serverModified,omitempty"`
}

// DropboxListOut wraps a folder listing (and search results — same envelope, so
// the client decodes both with one type). Path echoes the normalized folder the
// listing came from.
//
//deneb:wire
type DropboxListOut struct {
	Entries []DropboxEntryOut `json:"entries"`
	Path    string            `json:"path"`
}

// DropboxShareOut carries a shareable URL for a file.
//
//deneb:wire
type DropboxShareOut struct {
	URL string `json:"url"`
}

// DropboxUploadOut is the metadata of an uploaded file (autorename may have
// changed the name from the requested one).
//
//deneb:wire
type DropboxUploadOut struct {
	Entry DropboxEntryOut `json:"entry"`
}

// DropboxBrowseClient is the subset of *dropbox.Client the browser handlers use.
// Interface-based so tests can substitute a fake; *dropbox.Client satisfies it
// structurally.
type DropboxBrowseClient interface {
	ListFolder(ctx context.Context, path string, recursive bool, limit int) ([]dropbox.Entry, error)
	Search(ctx context.Context, query string, maxResults int) ([]dropbox.Entry, error)
	CreateSharedLink(ctx context.Context, path string) (string, error)
	Upload(ctx context.Context, destPath string, data []byte, overwrite bool) (*dropbox.Entry, error)
}

// DropboxBrowseDeps wires the browser RPCs to a lazy Dropbox client factory
// (mirrors Gmail/Calendar). A nil Client (no factory) skips the whole domain.
type DropboxBrowseDeps struct {
	Client func() (DropboxBrowseClient, error)
}

// DropboxBrowseMethods returns the miniapp.dropbox.{list,search,share,upload}
// handler map, or nil when no client factory is wired.
func DropboxBrowseMethods(deps DropboxBrowseDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.dropbox.list":   dropboxList(deps),
		"miniapp.dropbox.search": dropboxSearch(deps),
		"miniapp.dropbox.share":  dropboxShare(deps),
		"miniapp.dropbox.upload": dropboxUpload(deps),
	}
}

// --- list ----------------------------------------------------------------

func dropboxList(deps DropboxBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Path  string `json:"path,omitempty"`
		Limit int    `json:"limit,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		client, errResp := dropboxClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultDropboxListLimit
		}
		// ListFolder maps "/" → "" (the Dropbox root) internally; pass the path
		// through, non-recursive (folder-at-a-time browsing).
		path := strings.TrimSpace(p.Path)
		entries, err := client.ListFolder(ctx, path, false, limit)
		if err != nil {
			return mapDropboxError(req.ID, "dropbox list failed", err)
		}
		return rpcutil.RespondOK(req.ID, DropboxListOut{
			Entries: projectDropboxEntries(entries),
			Path:    path,
		})
	}
}

// --- search --------------------------------------------------------------

func dropboxSearch(deps DropboxBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Query string `json:"query"`
		Max   int    `json:"max,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.Query) == "" {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		client, errResp := dropboxClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		max := p.Max
		if max <= 0 {
			max = defaultDropboxSearchMax
		}
		entries, err := client.Search(ctx, p.Query, max)
		if err != nil {
			return mapDropboxError(req.ID, "dropbox search failed", err)
		}
		// Same envelope as list; Path is empty (results span folders, so the
		// client shows each hit's full pathDisplay instead).
		return rpcutil.RespondOK(req.ID, DropboxListOut{Entries: projectDropboxEntries(entries)})
	}
}

// --- share ---------------------------------------------------------------

func dropboxShare(deps DropboxBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.Path) == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		client, errResp := dropboxClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		// CreateSharedLink already handles the 409 "already exists" case by
		// fetching the existing link.
		link, err := client.CreateSharedLink(ctx, strings.TrimSpace(p.Path))
		if err != nil {
			return mapDropboxError(req.ID, "dropbox share failed", err)
		}
		return rpcutil.RespondOK(req.ID, DropboxShareOut{URL: link})
	}
}

// --- upload --------------------------------------------------------------

func dropboxUpload(deps DropboxBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Path       string `json:"path"`
		MimeType   string `json:"mimeType,omitempty"`
		DataBase64 string `json:"dataBase64"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		dest := strings.TrimSpace(p.Path)
		if dest == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		// Strip an optional data-URI prefix, then base64-decode (capture pattern).
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
			return rpcerr.InvalidParams(fmt.Errorf("dataBase64 is not valid base64")).Response(req.ID)
		}
		client, errResp := dropboxClientOrErr(deps, req.ID)
		if errResp != nil {
			return errResp
		}
		// overwrite=false → Dropbox autorenames on a name clash, so an upload never
		// clobbers an existing file. The platform enforces the 150 MiB single-shot
		// cap and returns a Korean error we surface verbatim.
		meta, err := client.Upload(ctx, dest, data, false)
		if err != nil {
			return mapDropboxError(req.ID, "dropbox upload failed", err)
		}
		var entry DropboxEntryOut
		if meta != nil {
			entry = projectDropboxEntry(*meta)
		}
		return rpcutil.RespondOK(req.ID, DropboxUploadOut{Entry: entry})
	}
}

// --- helpers -------------------------------------------------------------

func projectDropboxEntry(e dropbox.Entry) DropboxEntryOut {
	return DropboxEntryOut{
		Tag:            e.Tag,
		Name:           e.Name,
		PathDisplay:    e.PathDisplay,
		PathLower:      e.PathLower,
		ID:             e.ID,
		Size:           e.Size,
		ServerModified: e.ServerModified,
	}
}

func projectDropboxEntries(es []dropbox.Entry) []DropboxEntryOut {
	out := make([]DropboxEntryOut, 0, len(es))
	for _, e := range es {
		out = append(out, projectDropboxEntry(e))
	}
	return out
}

// dropboxClientOrErr resolves the lazy client factory, mapping a factory error
// (no token / not yet linked) to UNAVAILABLE so the native browser shows the
// "connect Dropbox" CTA instead of a generic failure.
func dropboxClientOrErr(deps DropboxBrowseDeps, reqID string) (DropboxBrowseClient, *protocol.ResponseFrame) {
	client, err := deps.Client()
	if err != nil {
		return nil, rpcerr.WrapUnavailable("dropbox not connected", err).Response(reqID)
	}
	return client, nil
}

// mapDropboxError classifies a Dropbox client error via the typed
// *dropbox.APIError so HTTP status drives the RPC error code rather than
// substring-matching the body.
func mapDropboxError(reqID, msg string, err error) *protocol.ResponseFrame {
	if err == nil {
		return rpcerr.Unavailable(msg).Response(reqID)
	}
	var apiErr *dropbox.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized:
			return rpcerr.New(protocol.ErrUnauthorized, msg+": "+apiErr.Error()).Response(reqID)
		case http.StatusForbidden:
			return rpcerr.New(protocol.ErrForbidden, msg+": "+apiErr.Error()).Response(reqID)
		case http.StatusNotFound:
			return rpcerr.NotFound(msg).Response(reqID)
		}
	}
	return rpcerr.WrapUnavailable(msg, err).Response(reqID)
}
