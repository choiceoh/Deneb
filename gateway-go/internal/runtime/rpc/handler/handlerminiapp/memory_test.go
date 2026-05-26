package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeMemoryStore struct {
	searchFn   func(ctx context.Context, q string, limit int) ([]wiki.SearchResult, error)
	readPageFn func(relPath string) (*wiki.Page, error)
}

func (f *fakeMemoryStore) Search(ctx context.Context, q string, n int) ([]wiki.SearchResult, error) {
	if f.searchFn == nil {
		return nil, errors.New("Search not stubbed")
	}
	return f.searchFn(ctx, q, n)
}

func (f *fakeMemoryStore) ReadPage(relPath string) (*wiki.Page, error) {
	if f.readPageFn == nil {
		return nil, errors.New("ReadPage not stubbed")
	}
	return f.readPageFn(relPath)
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
