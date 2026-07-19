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

package files

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/fileshare"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
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
//
// ExtractText turns a file's bytes into searchable text for the search RPC's
// content=true mode (the chat tools' document extractor). It is injected, not
// imported, because this handler package must never depend on pipeline/chat/tools
// (a layer inversion). A nil ExtractText silently degrades content search to a
// name-only search, so the feature is optional, not load-bearing.
//
// SemanticSearch ranks files by meaning (embedding vectors) for the search RPC's
// semantic=true mode. The server owns the embedding client + index and injects
// this closure; a nil func — or an empty result when the embedding server is
// down — falls back to name/content search, so semantic search is optional.
//
// OnDelete / OnMove / OnUpload keep the semantic index fresh after a mutation:
// without them, a deleted/moved file would still surface in semantic search
// (and 404 at download time) — and an overwrite-saved file would keep ranking
// by its OLD content — until the next 15-minute reindex. The server wires
// OnDelete and OnUpload to the shared index's Remove (an overwrite drops the
// stale vectors; the reindex re-embeds the new content) and OnMove to Rename.
// All are optional (nil = no-op); a Stat backstop in the search path also
// drops vanished hits, so these are a freshness optimization, not a
// correctness requirement.
type FilesBrowseDeps struct {
	Store          filestore.Store
	ExtractText    func(ctx context.Context, data []byte, name string) string
	SemanticSearch func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error)
	OnDelete       func(path string)
	OnMove         func(oldPath, newPath string)
	OnUpload       func(path string)
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
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
}

// --- search --------------------------------------------------------------

func filesBrowseSearch(deps FilesBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Query    string `json:"query"`
		Content  bool   `json:"content,omitempty"`
		Semantic bool   `json:"semantic,omitempty"`
		Max      int    `json:"max,omitempty"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if strings.TrimSpace(p.Query) == "" {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		max := p.Max
		if max <= 0 {
			max = defaultFilesSearchMax
		}
		// semantic=true ranks by meaning (vectors) when the index is wired. Empty
		// results — embedding server down — fall through to lexical search so the
		// request still returns useful hits offline (semantic is never required).
		if p.Semantic && deps.SemanticSearch != nil {
			if hits, serr := deps.SemanticSearch(ctx, p.Query, max); serr == nil && len(hits) > 0 {
				entries := make([]filestore.Entry, 0, len(hits))
				for _, h := range hits {
					entries = append(entries, h.Entry)
				}
				return rpcutil.RespondOK(req.ID, FilesListOut{Entries: projectFilesEntries(entries)})
			}
		}
		// content=true widens the match to extracted file text — but only when an
		// extractor is wired; otherwise fall back to the name-only Search so the
		// request still succeeds (just narrower) rather than erroring.
		var (
			entries []filestore.Entry
			err     error
		)
		if p.Content && deps.ExtractText != nil {
			entries, err = deps.Store.SearchContent(ctx, p.Query, max, deps.ExtractText)
		} else {
			entries, err = deps.Store.Search(ctx, p.Query, max)
		}
		if err != nil {
			return mapFilesError(req.ID, "file search failed", err)
		}
		// Same envelope as list; Path is empty (results span folders, so the
		// client shows each hit's full pathDisplay instead).
		return rpcutil.RespondOK(req.ID, FilesListOut{Entries: projectFilesEntries(entries)})
	})
}

// --- share ---------------------------------------------------------------

func filesBrowseShare(deps FilesBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
}

// --- upload --------------------------------------------------------------

func filesBrowseUpload(deps FilesBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Path     string `json:"path"`
		MimeType string `json:"mimeType,omitempty"`
		// Pointer distinguishes an ABSENT field (client bug — reject, even on
		// the overwrite path, where it used to silently truncate the file to
		// zero bytes) from an explicit empty string (editor clearing a text
		// file — legitimate, but only with overwrite=true).
		DataBase64 *string `json:"dataBase64"`
		// Overwrite replaces the file at Path in place — the desktop editor's
		// save path. Default false keeps the capture semantics (autorename on
		// name clash) so existing uploaders never clobber a file.
		Overwrite bool `json:"overwrite,omitempty"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		dest := strings.TrimSpace(p.Path)
		if dest == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		if p.DataBase64 == nil {
			return rpcerr.MissingParam("dataBase64").Response(req.ID)
		}
		// Strip an optional data-URI prefix, then base64-decode (capture pattern).
		raw := strings.TrimSpace(*p.DataBase64)
		if strings.HasPrefix(raw, "data:") {
			if i := strings.IndexByte(raw, ','); i > 0 {
				raw = raw[i+1:]
			}
		}
		// Decode the payload. Empty content is allowed ONLY on the editor save
		// path (overwrite=true) — clearing a text file to zero bytes is a
		// legitimate save. A capture upload (overwrite=false) with no bytes is a
		// botched capture, so it still errors rather than storing an empty file.
		var data []byte
		if raw != "" {
			decoded, derr := base64.StdEncoding.DecodeString(raw)
			if derr != nil {
				return rpcerr.InvalidParams(fmt.Errorf("dataBase64 is not valid base64")).Response(req.ID)
			}
			data = decoded
		}
		if len(data) == 0 && !p.Overwrite {
			return rpcerr.MissingParam("dataBase64").Response(req.ID)
		}
		// overwrite=false (default) → the store autorenames on a name clash, so
		// a capture upload never clobbers a file; overwrite=true is the editor
		// save, replacing the same path in place.
		meta, err := deps.Store.Put(ctx, dest, data, p.Overwrite)
		if err != nil {
			return mapFilesError(req.ID, "file upload failed", err)
		}
		var entry FilesEntryOut
		if meta != nil {
			entry = projectFilesEntry(*meta)
		}
		// An in-place replace leaves the semantic index holding the OLD
		// content's vectors for up to a 15-min reindex cycle — drop them so
		// semantic search stops ranking the file by stale text (it re-embeds
		// at the next pass; lexical search still finds it meanwhile). Fresh
		// uploads (autorenamed or new paths) have no entry — harmless no-op.
		if deps.OnUpload != nil && p.Overwrite && meta != nil {
			deps.OnUpload(meta.PathDisplay)
		}
		return rpcutil.RespondOK(req.ID, FilesUploadOut{Entry: entry})
	})
}

// --- delete --------------------------------------------------------------

func filesBrowseDelete(deps FilesBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		path := strings.TrimSpace(p.Path)
		if path == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		if err := deps.Store.Delete(ctx, path); err != nil {
			return mapFilesError(req.ID, "file delete failed", err)
		}
		// Drop any semantic-index entry for the deleted path so search stops
		// returning it before the next reindex. Folders aren't indexed, so a
		// folder delete is simply a no-op here.
		if deps.OnDelete != nil {
			deps.OnDelete(path)
		}
		// Empty OK envelope — the client refreshes the folder on success.
		return rpcutil.RespondOK(req.ID, struct{}{})
	})
}

// --- mkdir ---------------------------------------------------------------

func filesBrowseMkdir(deps FilesBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
	})
}

// --- move ----------------------------------------------------------------

func filesBrowseMove(deps FilesBrowseDeps) rpcutil.HandlerFunc {
	type params struct {
		Src string `json:"src"`
		Dst string `json:"dst"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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
		// Re-key the semantic index from the old path to the *actual* new path
		// (the store auto-renames on a clash, so trust meta.PathDisplay over the
		// requested dst). Keeps the moved file findable at its new location before
		// the next reindex.
		if deps.OnMove != nil && meta != nil {
			deps.OnMove(src, meta.PathDisplay)
		}
		return rpcutil.RespondOK(req.ID, entry)
	})
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
// is a malformed client path, so it surfaces as INVALID_REQUEST (a 4xx-class
// client error) rather than UNAVAILABLE (which implies a transient server fault
// the client should retry); any other failure degrades to UNAVAILABLE.
func mapFilesError(reqID, msg string, err error) *protocol.ResponseFrame {
	if err == nil {
		return rpcerr.Unavailable(msg).Response(reqID)
	}
	if errors.Is(err, os.ErrNotExist) {
		return rpcerr.NotFound(msg + ": " + err.Error()).Response(reqID)
	}
	if errors.Is(err, filestore.ErrPathEscape) {
		return rpcerr.WrapInvalidRequest(msg, err).Response(reqID)
	}
	return rpcerr.WrapUnavailable(msg, err).Response(reqID)
}
