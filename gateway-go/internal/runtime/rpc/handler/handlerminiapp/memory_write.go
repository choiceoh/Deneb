// memory_write.go — miniapp.memory.* write-side handlers: page write,
// create, merge, and delete. Split from memory.go (deps, registration,
// read handlers, shared helpers).
package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func memoryWritePage(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
		Body string `json:"body"`
		// Frontmatter overrides. Pointer-typed for title/summary so the
		// client can distinguish "absent" (preserve existing) from ""
		// (clear the field). Tags presence is detected via the raw map
		// pass below because []string can't distinguish nil from
		// omitted in encoding/json.
		Title   *string  `json:"title,omitempty"`
		Summary *string  `json:"summary,omitempty"`
		Tags    []string `json:"tags,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		var rawFields map[string]json.RawMessage
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
			// Second pass: figure out which fields were *present* in the
			// payload (so we can tell `omitted` apart from explicit-nil
			// or explicit-empty). Cheaper than chasing every field with
			// json.RawMessage.
			_ = json.Unmarshal(req.Params, &rawFields)
		}
		rel := strings.TrimSpace(p.Path)
		if rel == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		if err := validateWikiPath(rel); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}

		// ReadPage to preserve existing frontmatter for fields the
		// client didn't touch. Missing page → NOT_FOUND.
		existing, err := store.ReadPage(rel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(rel)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("wiki page read failed", err).Response(req.ID)
		}
		if existing == nil {
			return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(rel)).Response(req.ID)
		}

		existing.Body = p.Body
		if p.Title != nil {
			existing.Meta.Title = strings.TrimSpace(*p.Title)
		}
		if p.Summary != nil {
			existing.Meta.Summary = strings.TrimSpace(*p.Summary)
		}
		if _, ok := rawFields["tags"]; ok {
			// "tags": [] clears, "tags": ["a","b"] replaces. Trim each
			// and drop blanks so the file doesn't accumulate empty tags.
			cleaned := make([]string, 0, len(p.Tags))
			for _, t := range p.Tags {
				t = strings.TrimSpace(t)
				if t != "" {
					cleaned = append(cleaned, t)
				}
			}
			existing.Meta.Tags = cleaned
		}
		existing.Meta.Updated = todayDateString()

		if err := store.WritePage(rel, existing); err != nil {
			return rpcerr.WrapUnavailable("wiki page write failed", err).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, pageToOut(rel, existing))
	}
}

// memoryCreatePage creates a brand-new wiki page. Path is computed
// from category + a slugified title so the user doesn't have to think
// about filesystem layout. Returns CONFLICT (NOT_FOUND-not-quite —
// using INVALID_REQUEST since wire protocol lacks a dedicated conflict
// code) when the computed path already exists.
//
// Required: title, category. Optional: summary, tags, body. Created
// AND Updated are stamped to today; the operator can edit later.
func memoryCreatePage(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Title    string   `json:"title"`
		Category string   `json:"category"`
		Summary  string   `json:"summary,omitempty"`
		Tags     []string `json:"tags,omitempty"`
		Body     string   `json:"body,omitempty"`
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
		title := strings.TrimSpace(p.Title)
		category := strings.TrimSpace(p.Category)
		if title == "" {
			return rpcerr.MissingParam("title").Response(req.ID)
		}
		if category == "" {
			return rpcerr.MissingParam("category").Response(req.ID)
		}

		// Compute path: <category>/<slug>.md. The category itself goes
		// through the same path validation as the final relative path
		// (so "../" in the category gets rejected). Slug derives from
		// title: lowercase, non-alnum → "-", collapse repeats, trim.
		slug := slugifyTitle(title)
		if slug == "" {
			return rpcerr.InvalidRequest("title yields empty slug").Response(req.ID)
		}
		rel := path.Join(category, slug+".md")
		if err := validateWikiPath(rel); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}

		// Conflict check: if ReadPage succeeds the file already exists.
		// fs.ErrNotExist is the "all clear" signal here; everything else
		// is a real IO error and surfaces as UNAVAILABLE.
		if existing, rerr := store.ReadPage(rel); rerr == nil && existing != nil {
			return rpcerr.InvalidRequest("page already exists: " + rel).Response(req.ID)
		} else if rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			return rpcerr.WrapUnavailable("wiki page probe failed", rerr).Response(req.ID)
		}

		today := todayDateString()
		cleanedTags := make([]string, 0, len(p.Tags))
		for _, t := range p.Tags {
			t = strings.TrimSpace(t)
			if t != "" {
				cleanedTags = append(cleanedTags, t)
			}
		}
		page := &wiki.Page{
			Meta: wiki.Frontmatter{
				Title:    title,
				Summary:  strings.TrimSpace(p.Summary),
				Category: category,
				Tags:     cleanedTags,
				Created:  today,
				Updated:  today,
			},
			Body: p.Body,
		}

		if err := store.WritePage(rel, page); err != nil {
			return rpcerr.WrapUnavailable("wiki page write failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, pageToOut(rel, page))
	}
}

// memoryMergePage kicks off folding one wiki page into another and returns
// immediately — the merge runs in the BACKGROUND. The slow step is synthesizing
// the combined body with the lightweight model, so blocking the request on it
// made the Mini App spin (and time out when the model was slow/down). Instead
// the handler validates, confirms both pages exist, hands the two pages to
// deps.StartMerge, and replies "started"; when the background job completes
// (combined body written, every referencing page repointed, source deleted —
// or a concatenation fallback if the model is unavailable) the user gets a
// native completion notice.
//
// Drives the Mini App's "두 프로젝트 병합" action (category-page multi-select).
// Single-operator, last-write-wins, recoverable from git history.
func memoryMergePage(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		TargetPath string `json:"targetPath"`
		SourcePath string `json:"sourcePath"`
	}
	type out struct {
		OK          bool   `json:"ok"`
		Started     bool   `json:"started"`
		TargetPath  string `json:"targetPath"`
		MergedTitle string `json:"mergedTitle,omitempty"`
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
		target := strings.TrimSpace(p.TargetPath)
		source := strings.TrimSpace(p.SourcePath)
		if target == "" {
			return rpcerr.MissingParam("targetPath").Response(req.ID)
		}
		if source == "" {
			return rpcerr.MissingParam("sourcePath").Response(req.ID)
		}
		// Same traversal guard as get_page/write_page on both paths.
		if err := validateWikiPath(target); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		if err := validateWikiPath(source); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}
		if target == source {
			return rpcerr.InvalidRequest("cannot merge a page into itself").Response(req.ID)
		}

		if deps.StartMerge == nil {
			return rpcerr.WrapUnavailable("merge worker unavailable",
				errors.New("StartMerge not wired")).Response(req.ID)
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}

		// Read both pages up front so a typo / already-deleted page fails fast
		// with NOT_FOUND instead of being queued for a doomed background job,
		// and so the synthesizer gets the original bodies before the source is
		// deleted mid-merge.
		targetPage, err := store.ReadPage(target)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(target)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("wiki page read failed", err).Response(req.ID)
		}
		sourcePage, err := store.ReadPage(source)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(source)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("wiki page read failed", err).Response(req.ID)
		}

		deps.StartMerge(target, source, targetPage, sourcePage)

		return rpcutil.RespondOK(req.ID, out{
			OK:          true,
			Started:     true,
			TargetPath:  target,
			MergedTitle: targetPage.Meta.Title,
		})
	}
}

// memoryDeletePages deletes one or more wiki pages by path. Drives the
// category-page multi-select delete in the native client / Mini App. Each
// path runs through the same traversal guard as get_page; deletes are
// best-effort per page so one bad/missing path doesn't abort the rest, and
// the response reports both the deleted count and any per-path failures so
// the client can tell a partial success from a clean sweep.
//
// Single-operator, last-write-wins — deletes are recoverable from the wiki's
// git history, so there's no soft-delete / undo layer here.
func memoryDeletePages(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Paths []string `json:"paths"`
	}
	type failure struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	}
	type out struct {
		OK      bool      `json:"ok"`
		Deleted int       `json:"deleted"`
		Failed  []failure `json:"failed,omitempty"`
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
		if len(p.Paths) == 0 {
			return rpcerr.MissingParam("paths").Response(req.ID)
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}

		result := out{OK: true}
		for _, raw := range p.Paths {
			rel := strings.TrimSpace(raw)
			if rel == "" {
				continue
			}
			if verr := validateWikiPath(rel); verr != nil {
				result.Failed = append(result.Failed, failure{Path: rel, Error: verr.Error()})
				continue
			}
			if derr := store.DeletePage(rel); derr != nil {
				result.Failed = append(result.Failed, failure{Path: rel, Error: derr.Error()})
				continue
			}
			result.Deleted++
		}
		if len(result.Failed) > 0 {
			result.OK = false
		}
		return rpcutil.RespondOK(req.ID, result)
	}
}

// pageToOut shapes a wiki.Page as the JSON map the get_page /
// write_page / create_page handlers all return. Extracted because
// three callers share it and the field list will keep growing.
