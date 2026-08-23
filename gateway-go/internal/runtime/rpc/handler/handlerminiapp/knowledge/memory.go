// memory.go — miniapp.memory.* RPC surface: deps/interface, method
// registration, the read handlers (get page, full-text search), and the
// helpers shared with the write (memory_write.go) and browse
// (memory_browse.go) handlers.
package knowledge

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MemorySearcher is the subset of *wiki.Store the handler needs. Defined
// here so tests can drop in a fake without spinning up the real store.
// Name kept for compatibility — it now covers listing/stats/diary AND
// writes too, not strictly searching. *wiki.Store satisfies all of
// these naturally; tests provide a fake.
type MemorySearcher interface {
	Search(ctx context.Context, query string, limit int) ([]wiki.SearchResult, error)
	SearchDiary(ctx context.Context, query string, limit int) ([]wiki.DiaryHit, error)
	ReadPage(relPath string) (*wiki.Page, error)
	WritePage(relPath string, page *wiki.Page) error
	DeletePage(relPath string) error
	MovePage(from, to string) error
	Stats() wiki.StoreStats
	ListPages(category string) ([]string, error)
	RecentDiaryEntries(limit int) []wiki.DiaryHit
}

// memoryQuerySearcher is an optional operator-facing extension implemented by
// *wiki.Store. Keeping it out of MemorySearcher preserves the small fake/store
// contract used by normal Mini App consumers while allowing explicit search
// diagnostics and stage ablations when requested.
type memoryQuerySearcher interface {
	SearchWithOptions(ctx context.Context, query string, limit int, options wiki.QueryOptions) (wiki.SearchReport, error)
}

type memoryPlanSearcher interface {
	SearchPlan(ctx context.Context, plan wiki.QueryPlan, limit int) (wiki.SearchReport, error)
}

// MemorySearchHit is the public miniapp.memory.search row. Fact-plane hits use
// the same ranked stream as wiki pages, but they are immutable evidence rather
// than page paths: ResultKind and ReadOnly keep clients from sending their
// synthetic paths to page mutation RPCs.
type MemorySearchHit struct {
	Path       string                  `json:"path"`
	Line       int                     `json:"line,omitempty"`
	EndLine    int                     `json:"endLine,omitempty"`
	Title      string                  `json:"title,omitempty"`
	Summary    string                  `json:"summary,omitempty"`
	Category   string                  `json:"category,omitempty"`
	Context    []string                `json:"context,omitempty"`
	Snippet    string                  `json:"snippet"`
	Score      float64                 `json:"score"`
	ResultKind string                  `json:"resultKind"`
	ReadOnly   bool                    `json:"readOnly"`
	FactID     string                  `json:"factId,omitempty"`
	SubjectID  string                  `json:"subjectId,omitempty"`
	Explain    *wiki.SearchExplanation `json:"explain,omitempty"`
}

// memoryProjectAliaser is an optional extension implemented by *wiki.Store: the
// Korean display name of the project owning a wiki path. Kept out of
// MemorySearcher for the same reason as the other extensions here — test fakes
// keep the small contract, and a fake without it simply yields no alias.
type memoryProjectAliaser interface {
	ProjectDisplayLabel(path string) string
}

// MemoryDeps holds the wiki store and is consumed at registration time.
// Store is a lazy factory so the gateway boots cleanly when the wiki
// knowledge base is disabled (per-config `wiki.enabled=false`); the
// handlers then surface UNAVAILABLE per call instead of crashing at boot.
type MemoryDeps struct {
	Store func() (MemorySearcher, error)

	// StartMerge launches a project-page merge in the BACKGROUND and returns
	// immediately. The slow part — synthesizing the combined body with the
	// lightweight model — runs off the request path with a generous timeout;
	// when it finishes (or falls back to concatenation, or fails) the user is
	// notified through the native client. The handler hands over the two
	// already-read pages so the synthesizer has the original bodies before the
	// source is deleted.
	// Wired by the server (see makeWikiMergeStarter); nil in tests / when the
	// merge worker is unavailable, in which case merge requests get UNAVAILABLE.
	StartMerge func(targetPath, sourcePath string, target, source *wiki.Page)
}

const (
	defaultMemorySearchLimit = 10
	maxMemorySearchLimit     = 50
	maxMemorySnippetChars    = 240

	// Caps for the new listing endpoints. The Mini App is mobile-first
	// so default page sizes stay small; the ceiling still bounds a
	// misbehaving client but admits the whole wiki in one call — the
	// desktop (Andromeda) folder tree renders the full corpus (~450
	// pages today, tiny rows) from a single list_in_category request.
	defaultMemoryListLimit = 50
	maxMemoryListLimit     = 2000
	defaultDiaryRecent     = 20
	maxDiaryRecent         = 100
	maxDiarySnippetChars   = 200
)

// MemoryMethods returns the miniapp.memory.* handler map. Returns nil if
// no store factory is provided so method_registry can register
// conditionally.
func MemoryMethods(deps MemoryDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.memory.search":           memorySearch(deps),
		"miniapp.memory.get_page":         memoryGetPage(deps),
		"miniapp.memory.write_page":       memoryWritePage(deps),
		"miniapp.memory.create_page":      memoryCreatePage(deps),
		"miniapp.memory.merge":            memoryMergePage(deps),
		"miniapp.memory.delete_pages":     memoryDeletePages(deps),
		"miniapp.memory.move_page":        memoryMovePage(deps),
		"miniapp.memory.categories":       memoryCategories(deps),
		"miniapp.memory.list_in_category": memoryListInCategory(deps),
		"miniapp.memory.mirror":           memoryMirror(deps),
		"miniapp.memory.diary_recent":     memoryDiaryRecent(deps),
		"miniapp.memory.diary_mirror":     memoryDiaryMirror(deps),
	}
}

// memoryGetPage returns the full body + frontmatter of a single wiki page.
// Used by the Mini App's wiki detail view when a memory search hit or a
// sender-context chip is tapped.
func memoryGetPage(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
	}
	type out struct {
		Path     string `json:"path"`
		Title    string `json:"title,omitempty"`
		Summary  string `json:"summary,omitempty"`
		Category string `json:"category,omitempty"`
		// Frozen project code (e.g. pl1-wdo-dev-001): anchors the deal notebook
		// (notebook.DealRef == code), so a project page can link to its raw-evidence
		// notebook. Empty for non-project pages.
		Code       string   `json:"code,omitempty"`
		Tags       []string `json:"tags,omitempty"`
		Related    []string `json:"related,omitempty"`
		Created    string   `json:"created,omitempty"`
		Updated    string   `json:"updated,omitempty"`
		Due        string   `json:"due,omitempty"`
		Importance float64  `json:"importance,omitempty"`
		// Structural metadata a reader needs to judge a page: which deal stage
		// it is in, whose account it belongs to, where the site is, what kind of
		// business it is, and whether it is still current. The clients showed
		// only the body, so a page with an empty stage or a stale
		// superseded_by flag looked exactly like a healthy one.
		Stage        string   `json:"stage,omitempty"`
		Client       string   `json:"client,omitempty"`
		Sites        []string `json:"sites,omitempty"`
		Kinds        []string `json:"kinds,omitempty"`
		Program      string   `json:"program,omitempty"`
		Sources      []string `json:"sources,omitempty"`
		SupersededBy string   `json:"supersededBy,omitempty"`
		Archived     bool     `json:"archived,omitempty"`
		Body         string   `json:"body"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		rel := strings.TrimSpace(p.Path)
		if rel == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		// Reject anything that could let the caller escape the wiki
		// root. Substring ".." alone is too weak — Store.ReadPage
		// does filepath.Join(s.dir, rel) so absolute paths and "../"
		// segments both need explicit guards. Backslash normalized to
		// forward slash before cleaning so Windows-style traversal
		// can't sneak past path.Clean.
		if err := validateWikiPath(rel); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}
		page, err := store.ReadPage(rel)
		if err != nil {
			// Distinguish "missing" from "unreadable" so the Mini App
			// surfaces the right banner — permission/IO errors used to
			// be misreported as NOT_FOUND and the client gave up.
			if errors.Is(err, fs.ErrNotExist) {
				return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(rel)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("wiki page read failed", err).Response(req.ID)
		}
		if page == nil {
			return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(rel)).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, out{
			Path:         rel,
			Title:        page.Meta.Title,
			Summary:      page.Meta.Summary,
			Category:     page.Meta.Category,
			Code:         page.Meta.Code,
			Tags:         page.Meta.Tags,
			Related:      page.Meta.Related,
			Created:      page.Meta.Created,
			Updated:      page.Meta.Updated,
			Due:          page.Meta.Due,
			Importance:   page.Meta.Importance,
			Stage:        page.Meta.Stage,
			Client:       page.Meta.Client,
			Sites:        page.Meta.Sites,
			Kinds:        page.Meta.Kinds,
			Program:      page.Meta.Program,
			Sources:      page.Meta.Sources,
			SupersededBy: page.Meta.SupersededBy,
			Archived:     page.Meta.Archived,
			Body:         page.Body,
		})
	})
}

func memorySearch(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Query   string             `json:"query"`
		Plan    string             `json:"plan,omitempty"`
		Clauses []wiki.QueryClause `json:"clauses,omitempty"`
		Scopes  []string           `json:"scopes,omitempty"`
		Limit   int                `json:"limit,omitempty"`
		Mode    wiki.SearchMode    `json:"mode,omitempty"`
		Explain bool               `json:"explain,omitempty"`
		Intent  string             `json:"intent,omitempty"`
		Rerank  bool               `json:"rerank,omitempty"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		query := strings.TrimSpace(p.Query)
		planRequested := strings.TrimSpace(p.Plan) != "" || len(p.Clauses) > 0 || len(p.Scopes) > 0
		if query == "" && !planRequested {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultMemorySearchLimit
		}
		if limit > maxMemorySearchLimit {
			limit = maxMemorySearchLimit
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory search unavailable", err).Response(req.ID)
		}
		optionsRequested := p.Mode != "" || p.Explain || strings.TrimSpace(p.Intent) != "" || p.Rerank
		var (
			results     []wiki.SearchResult
			diagnostics *wiki.SearchDiagnostics
		)
		if planRequested {
			searcher, ok := store.(memoryPlanSearcher)
			if !ok {
				return rpcerr.Unavailable("memory query plans unavailable").Response(req.ID)
			}
			plan := wiki.ParseQueryPlan(p.Plan)
			plan.Clauses = append(plan.Clauses, p.Clauses...)
			if len(plan.Clauses) == 0 && query != "" {
				plan = wiki.ParseQueryPlan(query)
			}
			if strings.TrimSpace(p.Intent) != "" {
				plan.Intent = strings.TrimSpace(p.Intent)
			}
			plan.Scopes = append(plan.Scopes, p.Scopes...)
			plan.Explain = p.Explain
			plan.ForceRerank = p.Rerank
			report, searchErr := searcher.SearchPlan(ctx, plan, limit)
			if searchErr != nil {
				return rpcerr.WrapUnavailable("memory search failed", searchErr).Response(req.ID)
			}
			results = report.Results
			diagnostics = &report.Diagnostics
		} else if optionsRequested {
			searcher, ok := store.(memoryQuerySearcher)
			if !ok {
				return rpcerr.Unavailable("memory search diagnostics unavailable").Response(req.ID)
			}
			report, searchErr := searcher.SearchWithOptions(ctx, query, limit, wiki.QueryOptions{
				Mode:        p.Mode,
				Explain:     p.Explain,
				Intent:      strings.TrimSpace(p.Intent),
				ForceIntent: p.Rerank,
				ForceRerank: p.Rerank,
			})
			if searchErr != nil {
				return rpcerr.WrapUnavailable("memory search failed", searchErr).Response(req.ID)
			}
			results = report.Results
			diagnostics = &report.Diagnostics
		} else {
			results, err = store.Search(ctx, query, limit)
		}
		if err != nil {
			return rpcerr.WrapUnavailable("memory search failed", err).Response(req.ID)
		}

		out := make([]MemorySearchHit, 0, len(results))
		for _, r := range results {
			row := MemorySearchHit{
				Path: r.Path, Line: r.Line, EndLine: r.EndLine,
				Context: r.Context, Snippet: truncateRunes(r.Content, maxMemorySnippetChars),
				Score: r.Score, Explain: r.Explain,
				ResultKind: "page", FactID: r.FactID, SubjectID: r.SubjectID,
			}
			if r.FactID != "" {
				row.ResultKind = "fact"
				row.ReadOnly = true
				out = append(out, row)
				continue
			}
			// Best-effort title/summary lookup. If reading the page
			// fails, fall through — Path + Snippet are still useful.
			if page, perr := store.ReadPage(r.Path); perr == nil && page != nil {
				row.Title = page.Meta.Title
				row.Summary = page.Meta.Summary
				row.Category = page.Meta.Category
			}
			out = append(out, row)
		}
		response := map[string]any{"results": out}
		if diagnostics != nil {
			response["diagnostics"] = diagnostics
		}
		return rpcutil.RespondOK(req.ID, response)
	})
}

// memoryWritePage replaces the body of an existing wiki page and,
// optionally, selected frontmatter fields. Body is always required;
// title/summary/tags are pointer-typed in the param so the client can
// distinguish "leave unchanged" (omit / null) from "clear to empty"
// (provide ""). Updated date is always bumped to today.
//
// Category is intentionally NOT editable here — changing the category
// would require moving the file on disk (the on-disk path encodes the
// category as a subdirectory). Page creation is the right surface for
// that and lives in memoryCreatePage.
//
// Single-operator deployment, no versioning / optimistic locking —
// the underlying wiki.Store is the source of truth and last-write-wins.
// If we ever multi-tenant, this needs an etag-style guard; for now the
// risk is the operator clobbering their own pending edit, which is
// recoverable from git history.
func pageToOut(rel string, page *wiki.Page) map[string]any {
	out := map[string]any{
		"path": rel,
		"body": page.Body,
	}
	// omitempty parity with the original anonymous structs — empty
	// strings / slices stay absent from the JSON.
	if page.Meta.Title != "" {
		out["title"] = page.Meta.Title
	}
	if page.Meta.Summary != "" {
		out["summary"] = page.Meta.Summary
	}
	if page.Meta.Category != "" {
		out["category"] = page.Meta.Category
	}
	// Frozen project code (e.g. pl1-wdo-dev-001): the stable identity that also
	// anchors the deal's notebook (notebook.DealRef). Surfacing it lets the native
	// wiki page link to its raw-evidence notebook — curated facts ↔ raw evidence,
	// joined by one code that survives page moves/reclassification.
	if page.Meta.Code != "" {
		out["code"] = page.Meta.Code
	}
	if len(page.Meta.Tags) > 0 {
		out["tags"] = page.Meta.Tags
	}
	if len(page.Meta.Related) > 0 {
		out["related"] = page.Meta.Related
	}
	if page.Meta.Created != "" {
		out["created"] = page.Meta.Created
	}
	if page.Meta.Updated != "" {
		out["updated"] = page.Meta.Updated
	}
	if page.Meta.Due != "" {
		out["due"] = page.Meta.Due
	}
	if page.Meta.Importance != 0 {
		out["importance"] = page.Meta.Importance
	}
	return out
}

// slugifyTitle reduces a human-readable title to a filesystem-safe
// slug. Korean and other non-ASCII letters are preserved (the wiki
// store handles UTF-8 paths fine on the host filesystem); only
// punctuation and whitespace collapse to hyphens.
func slugifyTitle(title string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.TrimSpace(title) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			prevHyphen = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + 32) // lowercase
			prevHyphen = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case r > 0x7F:
			// Non-ASCII (Korean, CJK, etc.) — pass through unchanged.
			b.WriteRune(r)
			prevHyphen = false
		default:
			// Whitespace / punctuation → hyphen, collapsed.
			if !prevHyphen && b.Len() > 0 {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// todayDateString returns YYYY-MM-DD in the gateway's local zone.
// Pulled out of memoryWritePage so the test can swap in a fixed clock.
// Production reads the wall clock directly; tests inject by overriding
// the package-level `nowFunc` (see memory_test.go).
var nowFunc = time.Now

func todayDateString() string {
	return nowFunc().Format("2006-01-02")
}

func truncateRunes(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars]) + "…"
}

// validateWikiPath rejects any rel value that could let a caller of
// miniapp.memory.get_page read outside the wiki directory. The wiki
// store does filepath.Join(dir, rel) which preserves an embedded
// absolute path on some platforms and joins relative paths normally —
// so we (a) reject absolute forms outright, (b) normalize backslashes
// to forward slashes, (c) Clean the result, and (d) ensure the cleaned
// path stays under the root.
func validateWikiPath(rel string) error {
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return errors.New("path must be relative to the wiki root")
	}
	// Windows-style C:\foo or D:\bar — reject up front so path.Clean
	// can't normalize the drive letter away.
	if len(rel) >= 2 && rel[1] == ':' {
		return errors.New("path must be relative to the wiki root")
	}
	normalized := strings.ReplaceAll(rel, "\\", "/")
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("path must stay within the wiki root")
	}
	return nil
}
