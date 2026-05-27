package handlerminiapp

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeMemoryStore struct {
	searchFn      func(ctx context.Context, q string, limit int) ([]wiki.SearchResult, error)
	searchDiaryFn func(ctx context.Context, q string, limit int) ([]wiki.DiaryHit, error)
	readPageFn    func(relPath string) (*wiki.Page, error)
	writePageFn   func(relPath string, page *wiki.Page) error
	statsFn       func() wiki.StoreStats
	listPagesFn   func(category string) ([]string, error)
	diaryRecentFn func(limit int) []wiki.DiaryHit
}

func (f *fakeMemoryStore) Search(ctx context.Context, q string, n int) ([]wiki.SearchResult, error) {
	if f.searchFn == nil {
		return nil, errors.New("Search not stubbed")
	}
	return f.searchFn(ctx, q, n)
}

func (f *fakeMemoryStore) SearchDiary(ctx context.Context, q string, n int) ([]wiki.DiaryHit, error) {
	if f.searchDiaryFn == nil {
		return nil, errors.New("SearchDiary not stubbed")
	}
	return f.searchDiaryFn(ctx, q, n)
}

func (f *fakeMemoryStore) ReadPage(relPath string) (*wiki.Page, error) {
	if f.readPageFn == nil {
		return nil, errors.New("ReadPage not stubbed")
	}
	return f.readPageFn(relPath)
}

func (f *fakeMemoryStore) WritePage(relPath string, page *wiki.Page) error {
	if f.writePageFn == nil {
		return errors.New("WritePage not stubbed")
	}
	return f.writePageFn(relPath, page)
}

func (f *fakeMemoryStore) Stats() wiki.StoreStats {
	if f.statsFn == nil {
		return wiki.StoreStats{}
	}
	return f.statsFn()
}

func (f *fakeMemoryStore) ListPages(category string) ([]string, error) {
	if f.listPagesFn == nil {
		return nil, errors.New("ListPages not stubbed")
	}
	return f.listPagesFn(category)
}

func (f *fakeMemoryStore) RecentDiaryEntries(limit int) []wiki.DiaryHit {
	if f.diaryRecentFn == nil {
		return nil
	}
	return f.diaryRecentFn(limit)
}

func memoryDepsFor(store MemorySearcher) MemoryDeps {
	return MemoryDeps{Store: func() (MemorySearcher, error) { return store, nil }}
}

func TestMemorySearch_HappyPath(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, q string, n int) ([]wiki.SearchResult, error) {
			if q != "dgx" || n != defaultMemorySearchLimit {
				t.Errorf("Search args: q=%q n=%d, want dgx/10", q, n)
			}
			return []wiki.SearchResult{
				{Path: "dgx-spark.md", Content: "DGX Spark is a powerful AI machine.", Score: 0.91},
			}, nil
		},
		readPageFn: func(p string) (*wiki.Page, error) {
			if p != "dgx-spark.md" {
				t.Errorf("ReadPage path = %q", p)
			}
			return &wiki.Page{
				Meta: wiki.Frontmatter{
					Title:    "DGX Spark",
					Summary:  "NVIDIA's AI workstation",
					Category: "hardware",
				},
			}, nil
		},
	}
	h := memorySearch(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.search", map[string]any{"query": "dgx"}))

	var got struct {
		Results []map[string]any `json:"results"`
	}
	decode(t, resp, &got)
	if len(got.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(got.Results))
	}
	r := got.Results[0]
	if r["title"] != "DGX Spark" || r["summary"] != "NVIDIA's AI workstation" || r["category"] != "hardware" {
		t.Errorf("metadata not enriched: %+v", r)
	}
	if score, _ := r["score"].(float64); score < 0.9 {
		t.Errorf("score = %v, want >= 0.9", r["score"])
	}
}

func TestMemorySearch_MissingQuery(t *testing.T) {
	store := &fakeMemoryStore{}
	h := memorySearch(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.search", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestMemorySearch_BlankQuery(t *testing.T) {
	store := &fakeMemoryStore{}
	h := memorySearch(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.search", map[string]any{"query": "   "}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestMemorySearch_LimitClamp(t *testing.T) {
	var seenLimit int
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, _ string, n int) ([]wiki.SearchResult, error) {
			seenLimit = n
			return nil, nil
		},
	}
	h := memorySearch(memoryDepsFor(store))
	h(authedCtx(), reqWith(t, "miniapp.memory.search", map[string]any{"query": "x", "limit": 9999}))
	if seenLimit != maxMemorySearchLimit {
		t.Errorf("limit = %d, want clamped to %d", seenLimit, maxMemorySearchLimit)
	}
}

func TestMemorySearch_ReadPageFailsFallsThrough(t *testing.T) {
	// ReadPage returning an error should not abort the search — Path +
	// Snippet are still returned to the client without Title/Summary.
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, _ string, _ int) ([]wiki.SearchResult, error) {
			return []wiki.SearchResult{{Path: "p.md", Content: "snip", Score: 0.5}}, nil
		},
		readPageFn: func(_ string) (*wiki.Page, error) {
			return nil, errors.New("io error")
		},
	}
	h := memorySearch(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.search", map[string]any{"query": "x"}))

	var got struct {
		Results []map[string]any `json:"results"`
	}
	decode(t, resp, &got)
	if len(got.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(got.Results))
	}
	if _, ok := got.Results[0]["title"]; ok {
		t.Errorf("expected title to be omitted when ReadPage fails: %+v", got.Results[0])
	}
	if got.Results[0]["path"] != "p.md" {
		t.Errorf("path missing: %+v", got.Results[0])
	}
}

func TestMemorySearch_RequiresAuth(t *testing.T) {
	h := memorySearch(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(context.Background(), reqWith(t, "miniapp.memory.search", map[string]any{"query": "x"}))
	if resp.OK {
		t.Fatalf("expected unauthorized, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestMemorySearch_StoreUnavailable(t *testing.T) {
	deps := MemoryDeps{
		Store: func() (MemorySearcher, error) {
			return nil, errors.New("wiki disabled")
		},
	}
	h := memorySearch(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.search", map[string]any{"query": "x"}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnavailable)
	}
}

func TestMemoryMethods_NilStoreReturnsNil(t *testing.T) {
	if got := MemoryMethods(MemoryDeps{Store: nil}); got != nil {
		t.Errorf("MemoryMethods(nil) = %v, want nil", got)
	}
}

// --- get_page -----------------------------------------------------------

func TestMemoryGetPage_HappyPath(t *testing.T) {
	store := &fakeMemoryStore{
		readPageFn: func(p string) (*wiki.Page, error) {
			if p != "people/alice.md" {
				t.Errorf("path = %q", p)
			}
			return &wiki.Page{
				Meta: wiki.Frontmatter{
					Title:      "Alice",
					Summary:    "Sales contact",
					Category:   "사람",
					Tags:       []string{"client", "topworks"},
					Related:    []string{"acme.md"},
					Updated:    "2026-05-26",
					Importance: 0.8,
				},
				Body: "# Alice\n\nFull notes here.",
			}, nil
		},
	}
	h := memoryGetPage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.get_page", map[string]any{
		"path": "people/alice.md",
	}))
	var got map[string]any
	decode(t, resp, &got)
	if got["title"] != "Alice" || got["category"] != "사람" {
		t.Errorf("metadata wrong: %+v", got)
	}
	if got["body"] == "" || got["body"] == nil {
		t.Errorf("body missing: %+v", got)
	}
	tags, _ := got["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("tags = %v, want 2", tags)
	}
}

func TestMemoryGetPage_MissingPath(t *testing.T) {
	h := memoryGetPage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.get_page", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want MISSING_PARAM", resp.Error.Code)
	}
}

func TestMemoryGetPage_PathTraversalRejected(t *testing.T) {
	h := memoryGetPage(memoryDepsFor(&fakeMemoryStore{}))
	// Each entry would let a caller escape the wiki root without the
	// pre-flight validation: parent traversal, absolute paths, Windows
	// drive letters, backslash variants, and pure "." / ".." stubs.
	for _, bad := range []string{
		"../etc/passwd",
		"people/../../secret",
		"/etc/hosts",
		"\\etc\\hosts",
		"C:\\Windows\\System32",
		"..\\..\\secret.md",
		".",
		"..",
	} {
		resp := h(authedCtx(), reqWith(t, "miniapp.memory.get_page", map[string]any{"path": bad}))
		if resp.OK {
			t.Errorf("path %q: expected error, got OK", bad)
			continue
		}
		if resp.Error.Code != protocol.ErrInvalidRequest {
			t.Errorf("path %q: code = %s, want INVALID_REQUEST", bad, resp.Error.Code)
		}
	}
}

func TestMemoryGetPage_NotFound(t *testing.T) {
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return nil, fs.ErrNotExist
		},
	}
	h := memoryGetPage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.get_page", map[string]any{"path": "x.md"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %s, want NOT_FOUND", resp.Error.Code)
	}
}

// Real IO/permission failures must not be misreported as NOT_FOUND —
// the client would stop retrying even though the page still exists.
// Anything that is not fs.ErrNotExist surfaces as UNAVAILABLE.
func TestMemoryGetPage_ReadFailureIsUnavailable(t *testing.T) {
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return nil, errors.New("permission denied")
		},
	}
	h := memoryGetPage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.get_page", map[string]any{"path": "x.md"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

func TestMemoryGetPage_RequiresAuth(t *testing.T) {
	h := memoryGetPage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(context.Background(), reqWith(t, "miniapp.memory.get_page", map[string]any{"path": "x.md"}))
	if resp.OK {
		t.Fatalf("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestMemoryMethods_RegistersAllMethods(t *testing.T) {
	got := MemoryMethods(memoryDepsFor(&fakeMemoryStore{}))
	for _, name := range []string{
		"miniapp.memory.search",
		"miniapp.memory.get_page",
		"miniapp.memory.write_page",
		"miniapp.memory.create_page",
		"miniapp.memory.categories",
		"miniapp.memory.list_in_category",
		"miniapp.memory.diary_recent",
	} {
		if _, ok := got[name]; !ok {
			t.Errorf("missing %q", name)
		}
	}
}

// --- write_page ---------------------------------------------------------

func TestMemoryWritePage_HappyPath(t *testing.T) {
	// Inject a fixed date so the test isn't time-dependent.
	prevNow := nowFunc
	nowFunc = func() time.Time {
		return time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	}
	defer func() { nowFunc = prevNow }()

	var capturedPath string
	var captured *wiki.Page
	store := &fakeMemoryStore{
		readPageFn: func(p string) (*wiki.Page, error) {
			if p != "people/alice.md" {
				t.Errorf("ReadPage path = %q", p)
			}
			return &wiki.Page{
				Meta: wiki.Frontmatter{
					Title:    "Alice",
					Summary:  "Sales contact",
					Category: "사람",
					Tags:     []string{"client"},
					Updated:  "2026-05-01",
				},
				Body: "old body",
			}, nil
		},
		writePageFn: func(p string, page *wiki.Page) error {
			capturedPath = p
			captured = page
			return nil
		},
	}
	h := memoryWritePage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.write_page", map[string]any{
		"path": "people/alice.md",
		"body": "new body",
	}))
	if !resp.OK {
		t.Fatalf("expected OK: %+v", resp.Error)
	}
	if capturedPath != "people/alice.md" {
		t.Errorf("write path = %q", capturedPath)
	}
	if captured.Body != "new body" {
		t.Errorf("body not replaced: %q", captured.Body)
	}
	if captured.Meta.Title != "Alice" || captured.Meta.Category != "사람" || captured.Meta.Summary != "Sales contact" {
		t.Errorf("frontmatter not preserved: %+v", captured.Meta)
	}
	if captured.Meta.Updated != "2026-05-27" {
		t.Errorf("updated not bumped: %q", captured.Meta.Updated)
	}
	if len(captured.Meta.Tags) != 1 || captured.Meta.Tags[0] != "client" {
		t.Errorf("tags not preserved: %v", captured.Meta.Tags)
	}

	// Response shape matches get_page output (body + meta).
	var got map[string]any
	decode(t, resp, &got)
	if got["body"] != "new body" || got["title"] != "Alice" {
		t.Errorf("response wrong: %+v", got)
	}
}

func TestMemoryWritePage_MissingPath(t *testing.T) {
	h := memoryWritePage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.write_page", map[string]any{"body": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM: %+v", resp)
	}
}

func TestMemoryWritePage_PathTraversalRejected(t *testing.T) {
	h := memoryWritePage(memoryDepsFor(&fakeMemoryStore{}))
	for _, bad := range []string{"../etc/passwd", "/etc/hosts", "..", "."} {
		resp := h(authedCtx(), reqWith(t, "miniapp.memory.write_page", map[string]any{
			"path": bad,
			"body": "pwn",
		}))
		if resp.OK {
			t.Errorf("path %q: expected error, got OK", bad)
			continue
		}
		if resp.Error.Code != protocol.ErrInvalidRequest {
			t.Errorf("path %q: code = %s, want INVALID_REQUEST", bad, resp.Error.Code)
		}
	}
}

func TestMemoryWritePage_NotFoundIfPageMissing(t *testing.T) {
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return nil, fs.ErrNotExist
		},
	}
	h := memoryWritePage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.write_page", map[string]any{
		"path": "missing.md",
		"body": "x",
	}))
	if resp.OK || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND: %+v", resp)
	}
}

func TestMemoryWritePage_WriteFailureIsUnavailable(t *testing.T) {
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Title: "X"}, Body: "old"}, nil
		},
		writePageFn: func(_ string, _ *wiki.Page) error {
			return errors.New("disk full")
		},
	}
	h := memoryWritePage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.write_page", map[string]any{
		"path": "a.md",
		"body": "x",
	}))
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE: %+v", resp)
	}
}

func TestMemoryWritePage_RequiresAuth(t *testing.T) {
	h := memoryWritePage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(context.Background(), reqWith(t, "miniapp.memory.write_page", map[string]any{
		"path": "a.md",
		"body": "x",
	}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("expected UNAUTHORIZED: %+v", resp)
	}
}

func TestMemoryWritePage_OverridesFrontmatter(t *testing.T) {
	prevNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC) }
	defer func() { nowFunc = prevNow }()

	var captured *wiki.Page
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{
				Meta: wiki.Frontmatter{
					Title:    "Old",
					Summary:  "Old summary",
					Category: "사람",
					Tags:     []string{"old"},
				},
				Body: "old body",
			}, nil
		},
		writePageFn: func(_ string, page *wiki.Page) error {
			captured = page
			return nil
		},
	}
	h := memoryWritePage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.write_page", map[string]any{
		"path":    "people/alice.md",
		"body":    "new body",
		"title":   "New",
		"summary": "New summary",
		"tags":    []any{"client", "vip", ""},
	}))
	if !resp.OK {
		t.Fatalf("expected OK: %+v", resp.Error)
	}
	if captured.Meta.Title != "New" || captured.Meta.Summary != "New summary" {
		t.Errorf("frontmatter not overridden: %+v", captured.Meta)
	}
	// Category cannot change via write_page.
	if captured.Meta.Category != "사람" {
		t.Errorf("category should be preserved: %q", captured.Meta.Category)
	}
	// Blank tag entry is dropped during cleanup.
	if len(captured.Meta.Tags) != 2 || captured.Meta.Tags[0] != "client" || captured.Meta.Tags[1] != "vip" {
		t.Errorf("tags not replaced cleanly: %v", captured.Meta.Tags)
	}
}

func TestMemoryWritePage_EmptyTagsListClears(t *testing.T) {
	prevNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC) }
	defer func() { nowFunc = prevNow }()

	var captured *wiki.Page
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Tags: []string{"old"}}, Body: "x"}, nil
		},
		writePageFn: func(_ string, page *wiki.Page) error {
			captured = page
			return nil
		},
	}
	h := memoryWritePage(memoryDepsFor(store))
	// "tags": [] is the explicit-clear case (vs omitting the key).
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.write_page", map[string]any{
		"path": "a.md",
		"body": "x",
		"tags": []any{},
	}))
	if !resp.OK {
		t.Fatalf("expected OK: %+v", resp.Error)
	}
	if len(captured.Meta.Tags) != 0 {
		t.Errorf("tags should be cleared: %v", captured.Meta.Tags)
	}
}

func TestMemoryWritePage_OmittedTagsPreserved(t *testing.T) {
	prevNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC) }
	defer func() { nowFunc = prevNow }()

	var captured *wiki.Page
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Tags: []string{"keep1", "keep2"}}, Body: "x"}, nil
		},
		writePageFn: func(_ string, page *wiki.Page) error {
			captured = page
			return nil
		},
	}
	h := memoryWritePage(memoryDepsFor(store))
	// No "tags" key at all → preserve existing.
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.write_page", map[string]any{
		"path": "a.md",
		"body": "x",
	}))
	if !resp.OK {
		t.Fatalf("expected OK: %+v", resp.Error)
	}
	if len(captured.Meta.Tags) != 2 {
		t.Errorf("tags should be preserved: %v", captured.Meta.Tags)
	}
}

// --- create_page --------------------------------------------------------

func TestMemoryCreatePage_HappyPath(t *testing.T) {
	prevNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC) }
	defer func() { nowFunc = prevNow }()

	var capturedPath string
	var captured *wiki.Page
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return nil, fs.ErrNotExist
		},
		writePageFn: func(p string, page *wiki.Page) error {
			capturedPath = p
			captured = page
			return nil
		},
	}
	h := memoryCreatePage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"title":    "Acme Corp",
		"category": "회사",
		"summary":  "Vendor of widgets",
		"tags":     []any{"vendor", "B2B", ""},
		"body":     "## 컨택\n\nAlice",
	}))
	if !resp.OK {
		t.Fatalf("expected OK: %+v", resp.Error)
	}
	if capturedPath != "회사/acme-corp.md" {
		t.Errorf("path = %q, want '회사/acme-corp.md'", capturedPath)
	}
	if captured.Meta.Title != "Acme Corp" || captured.Meta.Category != "회사" {
		t.Errorf("meta wrong: %+v", captured.Meta)
	}
	if captured.Meta.Created != "2026-05-27" || captured.Meta.Updated != "2026-05-27" {
		t.Errorf("dates wrong: created=%q updated=%q", captured.Meta.Created, captured.Meta.Updated)
	}
	if len(captured.Meta.Tags) != 2 {
		t.Errorf("tags blank not filtered: %v", captured.Meta.Tags)
	}
	if captured.Body != "## 컨택\n\nAlice" {
		t.Errorf("body wrong: %q", captured.Body)
	}

	var got map[string]any
	decode(t, resp, &got)
	if got["path"] != "회사/acme-corp.md" || got["title"] != "Acme Corp" {
		t.Errorf("response: %+v", got)
	}
}

func TestMemoryCreatePage_RejectsExisting(t *testing.T) {
	store := &fakeMemoryStore{
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Title: "Existing"}}, nil
		},
	}
	h := memoryCreatePage(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"title":    "Existing",
		"category": "사람",
	}))
	if resp.OK {
		t.Fatal("expected error for existing page")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("code = %s, want INVALID_REQUEST", resp.Error.Code)
	}
}

func TestMemoryCreatePage_MissingTitle(t *testing.T) {
	h := memoryCreatePage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"category": "사람",
	}))
	if resp.OK || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM(title): %+v", resp)
	}
}

func TestMemoryCreatePage_MissingCategory(t *testing.T) {
	h := memoryCreatePage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"title": "Alice",
	}))
	if resp.OK || resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("expected MISSING_PARAM(category): %+v", resp)
	}
}

func TestMemoryCreatePage_RejectsCategoryTraversal(t *testing.T) {
	h := memoryCreatePage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"title":    "Sneaky",
		"category": "../etc",
	}))
	if resp.OK {
		t.Fatal("expected error for traversal category")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestMemoryCreatePage_EmptySlugRejected(t *testing.T) {
	h := memoryCreatePage(memoryDepsFor(&fakeMemoryStore{}))
	// Title is all punctuation → slug ends up empty.
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"title":    "??!!",
		"category": "misc",
	}))
	if resp.OK {
		t.Fatal("expected error for empty slug")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestMemoryCreatePage_RequiresAuth(t *testing.T) {
	h := memoryCreatePage(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(context.Background(), reqWith(t, "miniapp.memory.create_page", map[string]any{
		"title":    "X",
		"category": "Y",
	}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("expected UNAUTHORIZED: %+v", resp)
	}
}

// --- slugifyTitle helper ------------------------------------------------

func TestSlugifyTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Acme Corp", "acme-corp"},
		{"Acme  Corp  ", "acme-corp"},
		{"Hello, World!", "hello-world"},
		{"DGX Spark v2", "dgx-spark-v2"},
		{"NVIDIA H100", "nvidia-h100"},
		{"한글 페이지", "한글-페이지"},
		{"홍길동", "홍길동"},
		{"  --leading-trailing--  ", "leading-trailing"},
		{"", ""},
		{"!!!", ""},
		{"a", "a"},
	}
	for _, c := range cases {
		got := slugifyTitle(c.in)
		if got != c.want {
			t.Errorf("slugifyTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- categories ---------------------------------------------------------

func TestMemoryCategories_HappyPath(t *testing.T) {
	store := &fakeMemoryStore{
		statsFn: func() wiki.StoreStats {
			return wiki.StoreStats{
				TotalPages: 7,
				TotalBytes: 1024,
				CategoryCount: map[string]int{
					"projects": 4,
					"people":   2,
					"(root)":   1,
				},
			}
		},
	}
	h := memoryCategories(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.categories", map[string]any{}))
	var got struct {
		Categories []map[string]any `json:"categories"`
		TotalPages int              `json:"totalPages"`
	}
	decode(t, resp, &got)
	if got.TotalPages != 7 {
		t.Errorf("totalPages = %d, want 7", got.TotalPages)
	}
	// Largest bucket should come first (page count desc).
	if got.Categories[0]["name"] != "projects" {
		t.Errorf("first category = %v, want projects", got.Categories[0]["name"])
	}
	if int(got.Categories[0]["pageCount"].(float64)) != 4 {
		t.Errorf("projects pageCount = %v, want 4", got.Categories[0]["pageCount"])
	}
}

func TestMemoryCategories_RequiresAuth(t *testing.T) {
	h := memoryCategories(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(context.Background(), reqWith(t, "miniapp.memory.categories", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("auth not enforced: %+v", resp)
	}
}

// --- list_in_category ---------------------------------------------------

func TestMemoryListInCategory_HappyPath(t *testing.T) {
	store := &fakeMemoryStore{
		listPagesFn: func(cat string) ([]string, error) {
			if cat != "projects" {
				t.Errorf("ListPages cat = %q, want projects", cat)
			}
			return []string{"projects/a.md", "projects/b.md"}, nil
		},
		readPageFn: func(p string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{
				Title:   "Title " + p,
				Updated: "2026-05-2" + string([]byte{p[len(p)-4]}), // 'a' or 'b'
			}}, nil
		},
	}
	h := memoryListInCategory(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.list_in_category", map[string]any{"category": "projects"}))
	var got struct {
		Category string           `json:"category"`
		Pages    []map[string]any `json:"pages"`
		Total    int              `json:"total"`
	}
	decode(t, resp, &got)
	if got.Category != "projects" || got.Total != 2 || len(got.Pages) != 2 {
		t.Fatalf("unexpected response: %+v", got)
	}
	if got.Pages[0]["title"] == "" {
		t.Errorf("title not enriched: %+v", got.Pages[0])
	}
}

func TestMemoryListInCategory_RootBucketMapsToEmpty(t *testing.T) {
	var seenCat string
	store := &fakeMemoryStore{
		listPagesFn: func(cat string) ([]string, error) {
			seenCat = cat
			return nil, nil
		},
	}
	h := memoryListInCategory(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.list_in_category", map[string]any{"category": "(root)"}))
	if !resp.OK {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if seenCat != "" {
		t.Errorf("ListPages got %q, want empty string for (root)", seenCat)
	}
}

func TestMemoryListInCategory_PathTraversalRejected(t *testing.T) {
	h := memoryListInCategory(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.list_in_category", map[string]any{"category": "../etc"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("code = %s, want INVALID_REQUEST", resp.Error.Code)
	}
}

func TestMemoryListInCategory_LimitClampedAndTotalReflectsAll(t *testing.T) {
	store := &fakeMemoryStore{
		listPagesFn: func(_ string) ([]string, error) {
			paths := make([]string, 300)
			for i := range paths {
				paths[i] = "p" + string([]byte{byte('a' + i%26)}) + ".md"
			}
			return paths, nil
		},
		readPageFn: func(_ string) (*wiki.Page, error) { return &wiki.Page{}, nil },
	}
	h := memoryListInCategory(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.list_in_category", map[string]any{"limit": 9999}))
	var got struct {
		Pages []map[string]any `json:"pages"`
		Total int              `json:"total"`
	}
	decode(t, resp, &got)
	if got.Total != 300 {
		t.Errorf("total = %d, want 300 (full set)", got.Total)
	}
	if len(got.Pages) != maxMemoryListLimit {
		t.Errorf("len(pages) = %d, want clamped to %d", len(got.Pages), maxMemoryListLimit)
	}
}

// --- diary_recent -------------------------------------------------------

func TestMemoryDiaryRecent_HappyPath(t *testing.T) {
	var seenLimit int
	store := &fakeMemoryStore{
		diaryRecentFn: func(limit int) []wiki.DiaryHit {
			seenLimit = limit
			return []wiki.DiaryHit{
				{File: "diary-2026-05-26.md", Header: "14:30", Content: "Met Alice", At: 1716000000000},
				{File: "diary-2026-05-25.md", Header: "09:00", Content: "Standup notes", At: 1715900000000},
			}
		},
	}
	h := memoryDiaryRecent(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.diary_recent", map[string]any{}))
	if seenLimit != defaultDiaryRecent {
		t.Errorf("default limit = %d, want %d", seenLimit, defaultDiaryRecent)
	}
	var got struct {
		Entries []map[string]any `json:"entries"`
	}
	decode(t, resp, &got)
	if len(got.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(got.Entries))
	}
	if got.Entries[0]["file"] != "diary-2026-05-26.md" {
		t.Errorf("first file = %v", got.Entries[0]["file"])
	}
}

func TestMemoryDiaryRecent_LimitClamp(t *testing.T) {
	var seenLimit int
	store := &fakeMemoryStore{
		diaryRecentFn: func(limit int) []wiki.DiaryHit {
			seenLimit = limit
			return nil
		},
	}
	h := memoryDiaryRecent(memoryDepsFor(store))
	h(authedCtx(), reqWith(t, "miniapp.memory.diary_recent", map[string]any{"limit": 9999}))
	if seenLimit != maxDiaryRecent {
		t.Errorf("clamp = %d, want %d", seenLimit, maxDiaryRecent)
	}
}

func TestMemoryDiaryRecent_TruncatesLongContent(t *testing.T) {
	longBody := strings.Repeat("가", maxDiarySnippetChars+50)
	store := &fakeMemoryStore{
		diaryRecentFn: func(_ int) []wiki.DiaryHit {
			return []wiki.DiaryHit{{File: "d.md", Header: "00:00", Content: longBody}}
		},
	}
	h := memoryDiaryRecent(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.diary_recent", map[string]any{}))
	var got struct {
		Entries []map[string]any `json:"entries"`
	}
	decode(t, resp, &got)
	content := got.Entries[0]["content"].(string)
	if !strings.HasSuffix(content, "…") {
		t.Errorf("expected truncation suffix, got %q", content[len(content)-20:])
	}
}

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		in     string
		max    int
		expect string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"가나다라마바사", 3, "가나다…"},
		{"", 5, ""},
	}
	for _, c := range cases {
		got := truncateRunes(c.in, c.max)
		if got != c.expect {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", c.in, c.max, got, c.expect)
		}
	}
}
