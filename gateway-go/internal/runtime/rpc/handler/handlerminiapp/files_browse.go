// files_browse.go — miniapp.files.* file-browser RPCs over the local file store.
//
//	miniapp.files.list    — list a folder's entries
//	miniapp.files.search  — search the store by name query
//	miniapp.files.share   — mint a signed, TTL-bounded download link for a path
//	miniapp.files.upload  — upload device bytes to a destination path
//	miniapp.files.delete  — remove a file or empty folder
//	miniapp.files.mkdir   — create a folder (parents included)
//	miniapp.files.move    — move/rename a path (a rename is a same-folder move)
//
// The local-disk replacement for miniapp.dropbox.* (dropbox_browse.go): filestore.Entry
// mirrors dropbox.Entry field-for-field, so this is the same browser shape over a
// local backend — no OAuth, no external API. "Analyze a file" is NOT here: it runs
// a full agent turn via the chat bridge, so this package never imports
// pipeline/chat/tools. Shares are signed links (fileshare), not provider links.

package handlerminiapp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/fileshare"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	defaultFilesListLimit = 200
	defaultFilesSearchMax = 50
)

// FilesEntryOut is one file/folder row. Mirrors DropboxEntryOut (and
// filestore.Entry) so the native client shares one shape across the cutover.
// Tag is "file" or "folder"; Size is bytes (the client formats it).
//
//deneb:wire
type FilesEntryOut struct {
	Tag            string `json:"tag"`
	Name           string `json:"name"`
	PathDisplay    string `json:"pathDisplay"`
	PathLower      string `json:"pathLower"`
	ID             string `json:"id,omitempty"`
	Size           int64  `json:"size,omitempty"`
	ServerModified string `json:"serverModified,omitempty"`
}

// FilesListOut wraps a folder listing (and search results — same envelope, so
// the client decodes both with one type). Path echoes the normalized folder the
// listing came from (empty for search, whose hits span folders).
//
//deneb:wire
type FilesListOut struct {
	Entries []FilesEntryOut `json:"entries"`
	Path    string          `json:"path"`
}

// FilesShareOut carries a signed, TTL-bounded download URL for a file.
//
//deneb:wire
type FilesShareOut struct {
	URL string `json:"url"`
}

// FilesUploadOut is the metadata of an uploaded file (autorename may have
// changed the name from the requested one).
//
//deneb:wire
type FilesUploadOut struct {
	Entry FilesEntryOut `json:"entry"`
}

// FilesBrowseDeps wires the browser RPCs to the local file store. A nil Store
// skips the whole domain (mirrors DropboxBrowseDeps' nil-client skip).
type FilesBrowseDeps struct {
	Store filestore.Store
}

// FilesBrowseMethods returns the
// miniapp.files.{list,search,share,upload,delete,mkdir,move} handler map, or nil
// when no store is wired.
func FilesBrowseMethods(deps FilesBrowseDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.files.list":   filesBrowseList(deps),
		"miniapp.files.search": filesBrowseSearch(deps),
		"miniapp.files.share":  filesBrowseShare(deps),
		"miniapp.files.upload": filesBrowseUpload(deps),
		"miniapp.files.delete": filesBrowseDelete(deps),
		"miniapp.files.mkdir":  filesBrowseMkdir(deps),
		"miniapp.files.move":   filesBrowseMove(deps),
	}
}

// --- list ----------------------------------------------------------------

func filesBrowseList(deps FilesBrowseDeps) rpcutil.HandlerFunc {
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
		limit := p.Limit
		if limit <= 0 {
			limit = defaultFilesListLimit
		}
		// Folder-at-a-time browsing (non-recursive); the store maps ""/"/" to root.
		path := strings.TrimSpace(p.Path)
		entries, err := deps.Store.List(ctx, path, false, limit)
		if err != nil {
			return mapFilesError(req.ID, "file list failed", err)
		}
		return rpcutil.RespondOK(req.ID, FilesListOut{
			Entries: projectFilesEntries(entries),
			Path:    path,
		})
	}
}

// --- search --------------------------------------------------------------

func filesBrowseSearch(deps FilesBrowseDeps) rpcutil.HandlerFunc {
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
		max := p.Max
		if max <= 0 {
			max = defaultFilesSearchMax
		}
		entries, err := deps.Store.Search(ctx, p.Query, max)
		if err != nil {
			return mapFilesError(req.ID, "file search failed", err)
		}
		// Same envelope as list; Path is empty (results span folders, so the
		// client shows each hit's full pathDisplay instead).
		return rpcutil.RespondOK(req.ID, FilesListOut{Entries: projectFilesEntries(entries)})
	}
}

// --- share ---------------------------------------------------------------

func filesBrowseShare(deps FilesBrowseDeps) rpcutil.HandlerFunc {
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
		path := strings.TrimSpace(p.Path)
		if path == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		// Confirm the file exists before minting a link — a link to a missing
		// path would only 404 at download time, so fail early with a clear error.
		if _, err := deps.Store.Stat(ctx, path); err != nil {
			return mapFilesError(req.ID, "file share failed", err)
		}
		link := fileshare.Link(path)
		if link == "" {
			// No public base URL configured (or no client token to sign with):
			// the file is still reachable in-app, but a sharable link can't be minted.
			return rpcerr.Unavailable("공유 링크를 만들 수 없습니다 (공개 URL 미설정)").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, FilesShareOut{URL: link})
	}
}

// --- upload --------------------------------------------------------------

func filesBrowseUpload(deps FilesBrowseDeps) rpcutil.HandlerFunc {
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
		// overwrite=false → the store autorenames on a name clash, so an upload
		// never clobbers an existing file.
		meta, err := deps.Store.Put(ctx, dest, data, false)
		if err != nil {
			return mapFilesError(req.ID, "file upload failed", err)
		}
		var entry FilesEntryOut
		if meta != nil {
			entry = projectFilesEntry(*meta)
		}
		return rpcutil.RespondOK(req.ID, FilesUploadOut{Entry: entry})
	}
}

// --- delete --------------------------------------------------------------

func filesBrowseDelete(deps FilesBrowseDeps) rpcutil.HandlerFunc {
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
		path := strings.TrimSpace(p.Path)
		if path == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		if err := deps.Store.Delete(ctx, path); err != nil {
			return mapFilesError(req.ID, "file delete failed", err)
		}
		// Empty OK envelope — the client refreshes the folder on success.
		return rpcutil.RespondOK(req.ID, struct{}{})
	}
}

// --- mkdir ---------------------------------------------------------------

func filesBrowseMkdir(deps FilesBrowseDeps) rpcutil.HandlerFunc {
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
		path := strings.TrimSpace(p.Path)
		if path == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		meta, err := deps.Store.Mkdir(ctx, path)
		if err != nil {
			return mapFilesError(req.ID, "file mkdir failed", err)
		}
		var entry FilesEntryOut
		if meta != nil {
			entry = projectFilesEntry(*meta)
		}
		return rpcutil.RespondOK(req.ID, entry)
	}
}

// --- move ----------------------------------------------------------------

func filesBrowseMove(deps FilesBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		src := strings.TrimSpace(p.Src)
		dst := strings.TrimSpace(p.Dst)
		if src == "" {
			return rpcerr.MissingParam("src").Response(req.ID)
		}
		if dst == "" {
			return rpcerr.MissingParam("dst").Response(req.ID)
		}
		meta, err := deps.Store.Move(ctx, src, dst)
		if err != nil {
			return mapFilesError(req.ID, "file move failed", err)
		}
		var entry FilesEntryOut
		if meta != nil {
			entry = projectFilesEntry(*meta)
		}
		return rpcutil.RespondOK(req.ID, entry)
	}
}

// --- helpers -------------------------------------------------------------

func projectFilesEntry(e filestore.Entry) FilesEntryOut {
	return FilesEntryOut{
		Tag:            e.Tag,
		Name:           e.Name,
		PathDisplay:    e.PathDisplay,
		PathLower:      e.PathLower,
		ID:             e.ID,
		Size:           e.Size,
		ServerModified: e.ServerModified,
	}
}

func projectFilesEntries(es []filestore.Entry) []FilesEntryOut {
	out := make([]FilesEntryOut, 0, len(es))
	for _, e := range es {
		out = append(out, projectFilesEntry(e))
	}
	return out
}

// mapFilesError maps a filestore error to an RPC error code. A missing path
// surfaces as NOT_FOUND (the store wraps fs.ErrNotExist); a path-escape attempt
// or any other failure degrades to UNAVAILABLE.
func mapFilesError(reqID, msg string, err error) *protocol.ResponseFrame {
	if err == nil {
		return rpcerr.Unavailable(msg).Response(reqID)
	}
	if errors.Is(err, os.ErrNotExist) {
		return rpcerr.NotFound(msg + ": " + err.Error()).Response(reqID)
	}
	return rpcerr.WrapUnavailable(msg, err).Response(reqID)
}
