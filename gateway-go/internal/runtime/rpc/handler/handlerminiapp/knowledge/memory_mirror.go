// memory_mirror.go — miniapp.memory.mirror: bulk page export powering the
// native client's offline wiki mirror. The client pulls the whole corpus in
// lexical-path pages (cursor = last path considered), then keeps the mirror
// current from wiki.changed sync events; this endpoint only needs to be a
// cheap, resumable full scan (554 pages / ~2.6MB today).
package knowledge

import (
	"context"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	defaultMemoryMirrorLimit = 100
	maxMemoryMirrorLimit     = 300
)

// memoryMirror returns full pages (frontmatter + body) in stable lexical path
// order, resuming strictly after the cursor path. Rows mirror memoryGetPage's
// shape so a mirrored page renders identically to a live get_page fetch.
func memoryMirror(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		// Cursor is the last path the previous call considered ("" = start).
		// Paths are opaque resume tokens: a page created behind the cursor
		// mid-scan is picked up by the next full refresh or its wiki.changed
		// event, so the scan itself never needs to restart.
		Cursor string `json:"cursor,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	type pageOut struct {
		Path     string   `json:"path"`
		Title    string   `json:"title,omitempty"`
		Summary  string   `json:"summary,omitempty"`
		Category string   `json:"category,omitempty"`
		Code     string   `json:"code,omitempty"`
		Tags     []string `json:"tags,omitempty"`
		Updated  string   `json:"updated,omitempty"`
		Body     string   `json:"body"`
	}
	type out struct {
		Pages      []pageOut `json:"pages"`
		NextCursor string    `json:"nextCursor,omitempty"`
		HasMore    bool      `json:"hasMore"`
		Total      int       `json:"total"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		limit := p.Limit
		if limit <= 0 {
			limit = defaultMemoryMirrorLimit
		}
		if limit > maxMemoryMirrorLimit {
			limit = maxMemoryMirrorLimit
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}
		paths, err := store.ListPages("")
		if err != nil {
			return rpcerr.WrapUnavailable("list pages failed", err).Response(req.ID)
		}
		sort.Strings(paths)
		total := len(paths)

		start := 0
		if c := strings.TrimSpace(p.Cursor); c != "" {
			start = sort.SearchStrings(paths, c)
			if start < len(paths) && paths[start] == c {
				start++
			}
		}

		pages := make([]pageOut, 0, limit)
		i := start
		for ; i < len(paths) && len(pages) < limit; i++ {
			rel := paths[i]
			// Best-effort: a page deleted or unreadable mid-scan is skipped,
			// not fatal — the mirror is eventually repaired by wiki.changed
			// events or the next full refresh.
			page, perr := store.ReadPage(rel)
			if perr != nil || page == nil {
				continue
			}
			pages = append(pages, pageOut{
				Path:     rel,
				Title:    page.Meta.Title,
				Summary:  page.Meta.Summary,
				Category: page.Meta.Category,
				Code:     page.Meta.Code,
				Tags:     page.Meta.Tags,
				Updated:  page.Meta.Updated,
				Body:     page.Body,
			})
		}
		res := out{Pages: pages, Total: total, HasMore: i < len(paths)}
		if res.HasMore {
			// Resume after the last path CONSIDERED (not last emitted) so
			// skipped-unreadable tails can't wedge the scan in a loop.
			res.NextCursor = paths[i-1]
		}
		return rpcutil.RespondOK(req.ID, res)
	})
}

const (
	defaultDiaryMirrorLimit = 300
	maxDiaryMirrorLimit     = 500
	// diaryMirrorCursorSep joins (file, header) into one opaque cursor token.
	// NUL never occurs in either part (filenames are diary-YYYY-MM-DD.md,
	// headers are HH:MM section titles).
	diaryMirrorCursorSep = "\x00"
)

// memoryDiaryMirror is the diary counterpart of memoryMirror: full entries
// (not the 200-rune diary_recent snippets) in stable (file, header) order with
// a resumable cursor, powering the native client's offline diary search
// (~2,350 entries / ~1.9MB today). Entries are already redacted at the write
// boundary (wiki.AppendDiaryTo), so mirroring them to the device adds no new
// exposure beyond the wiki mirror.
func memoryDiaryMirror(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		// Cursor is the "file\x00header" of the last entry considered ("" =
		// start). An entry appended behind the cursor mid-scan is picked up
		// by the next full refresh — the scan itself never restarts.
		Cursor string `json:"cursor,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	type entryOut struct {
		File    string `json:"file"`
		Header  string `json:"header"`
		Content string `json:"content"`
		At      int64  `json:"at,omitempty"`
	}
	type out struct {
		Entries    []entryOut `json:"entries"`
		NextCursor string     `json:"nextCursor,omitempty"`
		HasMore    bool       `json:"hasMore"`
		Total      int        `json:"total"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		limit := p.Limit
		if limit <= 0 {
			limit = defaultDiaryMirrorLimit
		}
		if limit > maxDiaryMirrorLimit {
			limit = maxDiaryMirrorLimit
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}
		all := store.RecentDiaryEntries(int(^uint(0) >> 1))
		sort.Slice(all, func(i, j int) bool {
			if all[i].File != all[j].File {
				return all[i].File < all[j].File
			}
			return all[i].Header < all[j].Header
		})
		total := len(all)

		start := 0
		if c := strings.TrimSpace(p.Cursor); c != "" {
			start = sort.Search(len(all), func(i int) bool {
				return all[i].File+diaryMirrorCursorSep+all[i].Header > c
			})
		}

		end := start + limit
		if end > total {
			end = total
		}
		entries := make([]entryOut, 0, end-start)
		for _, h := range all[start:end] {
			entries = append(entries, entryOut{File: h.File, Header: h.Header, Content: h.Content, At: h.At})
		}
		res := out{Entries: entries, Total: total, HasMore: end < total}
		if res.HasMore && end > start {
			last := all[end-1]
			res.NextCursor = last.File + diaryMirrorCursorSep + last.Header
		}
		return rpcutil.RespondOK(req.ID, res)
	})
}
