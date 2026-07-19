package knowledge

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// mirrorFakeStore returns a fake whose ListPages("") yields the given paths
// and whose ReadPage serves a body derived from the path.
func mirrorFakeStore(paths []string) *fakeMemoryStore {
	return &fakeMemoryStore{
		listPagesFn: func(category string) ([]string, error) {
			if category != "" {
				return nil, fmt.Errorf("unexpected category %q", category)
			}
			return paths, nil
		},
		readPageFn: func(rel string) (*wiki.Page, error) {
			return &wiki.Page{
				Meta: wiki.Frontmatter{Title: "T:" + rel, Updated: "2026-07-17"},
				Body: "body of " + rel,
			}, nil
		},
	}
}

type mirrorOut struct {
	Pages []struct {
		Path    string `json:"path"`
		Title   string `json:"title"`
		Updated string `json:"updated"`
		Body    string `json:"body"`
	} `json:"pages"`
	NextCursor string `json:"nextCursor"`
	HasMore    bool   `json:"hasMore"`
	Total      int    `json:"total"`
}

func TestMemoryMirror_PaginatesInLexicalOrderWithCursorResume(t *testing.T) {
	// Deliberately unsorted: the handler must impose lexical order so the
	// cursor is a stable resume token across calls.
	h := memoryMirror(memoryDepsFor(mirrorFakeStore([]string{"c.md", "a.md", "b.md"})))

	resp := h(authedCtx(), reqWith(t, "miniapp.memory.mirror", map[string]any{"limit": 2}))
	var first mirrorOut
	decode(t, resp, &first)
	if len(first.Pages) != 2 || first.Pages[0].Path != "a.md" || first.Pages[1].Path != "b.md" {
		t.Fatalf("first page = %+v", first.Pages)
	}
	if !first.HasMore || first.NextCursor != "b.md" || first.Total != 3 {
		t.Fatalf("first meta = %+v", first)
	}
	if first.Pages[0].Body != "body of a.md" || first.Pages[0].Title != "T:a.md" {
		t.Errorf("page content = %+v", first.Pages[0])
	}

	resp = h(authedCtx(), reqWith(t, "miniapp.memory.mirror", map[string]any{
		"cursor": first.NextCursor,
		"limit":  2,
	}))
	var second mirrorOut
	decode(t, resp, &second)
	if len(second.Pages) != 1 || second.Pages[0].Path != "c.md" {
		t.Fatalf("second page = %+v", second.Pages)
	}
	if second.HasMore || second.NextCursor != "" {
		t.Errorf("second meta = %+v", second)
	}
}

func TestMemoryMirror_SkipsUnreadablePagesButAdvancesCursor(t *testing.T) {
	store := mirrorFakeStore([]string{"a.md", "b.md", "c.md", "d.md"})
	store.readPageFn = func(rel string) (*wiki.Page, error) {
		if rel == "b.md" {
			return nil, fs.ErrNotExist // deleted mid-scan
		}
		return &wiki.Page{Body: "body of " + rel}, nil
	}
	h := memoryMirror(memoryDepsFor(store))

	// limit bounds EMITTED rows: the scan reads past the unreadable b to fill
	// the page (a, c), and the cursor resumes after the last path considered.
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.mirror", map[string]any{"limit": 2}))
	var got mirrorOut
	decode(t, resp, &got)
	if len(got.Pages) != 2 || got.Pages[0].Path != "a.md" || got.Pages[1].Path != "c.md" {
		t.Fatalf("pages = %+v", got.Pages)
	}
	if !got.HasMore || got.NextCursor != "c.md" {
		t.Fatalf("meta = %+v", got)
	}

	resp = h(authedCtx(), reqWith(t, "miniapp.memory.mirror", map[string]any{
		"cursor": got.NextCursor,
		"limit":  2,
	}))
	var rest mirrorOut
	decode(t, resp, &rest)
	if len(rest.Pages) != 1 || rest.Pages[0].Path != "d.md" || rest.HasMore {
		t.Fatalf("resume = %+v", rest)
	}
}

func TestMemoryMirror_EmptyWiki(t *testing.T) {
	h := memoryMirror(memoryDepsFor(mirrorFakeStore(nil)))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.mirror", map[string]any{}))
	var got mirrorOut
	decode(t, resp, &got)
	if len(got.Pages) != 0 || got.HasMore || got.Total != 0 {
		t.Fatalf("got = %+v", got)
	}
}

func TestMemoryMirror_StoreUnavailable(t *testing.T) {
	h := memoryMirror(MemoryDeps{Store: func() (MemorySearcher, error) {
		return nil, errors.New("wiki disabled")
	}})
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.mirror", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

func TestMemoryMirror_ListFailure(t *testing.T) {
	store := &fakeMemoryStore{listPagesFn: func(string) ([]string, error) {
		return nil, errors.New("io error")
	}}
	h := memoryMirror(memoryDepsFor(store))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.mirror", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want UNAVAILABLE", resp.Error.Code)
	}
}

type diaryMirrorOut struct {
	Entries []struct {
		File    string `json:"file"`
		Header  string `json:"header"`
		Content string `json:"content"`
		At      int64  `json:"at"`
	} `json:"entries"`
	NextCursor string `json:"nextCursor"`
	HasMore    bool   `json:"hasMore"`
	Total      int    `json:"total"`
}

func TestMemoryDiaryMirror_PaginatesInFileHeaderOrderWithCursorResume(t *testing.T) {
	// Deliberately recency-ordered (as RecentDiaryEntries returns) — the
	// handler must impose (file, header) order for a stable cursor.
	store := &fakeMemoryStore{diaryRecentFn: func(int) []wiki.DiaryHit {
		return []wiki.DiaryHit{
			{File: "diary-2026-07-19.md", Header: "09:00", Content: "c3", At: 3},
			{File: "diary-2026-07-18.md", Header: "17:30", Content: "c2", At: 2},
			{File: "diary-2026-07-18.md", Header: "08:15", Content: "c1", At: 1},
		}
	}}
	h := memoryDiaryMirror(memoryDepsFor(store))

	resp := h(authedCtx(), reqWith(t, "miniapp.memory.diary_mirror", map[string]any{"limit": 2}))
	var first diaryMirrorOut
	decode(t, resp, &first)
	if len(first.Entries) != 2 || first.Entries[0].Content != "c1" || first.Entries[1].Content != "c2" {
		t.Fatalf("first entries = %+v", first.Entries)
	}
	if !first.HasMore || first.Total != 3 || first.NextCursor == "" {
		t.Fatalf("first meta = %+v", first)
	}

	resp = h(authedCtx(), reqWith(t, "miniapp.memory.diary_mirror", map[string]any{
		"cursor": first.NextCursor,
		"limit":  2,
	}))
	var second diaryMirrorOut
	decode(t, resp, &second)
	if len(second.Entries) != 1 || second.Entries[0].Content != "c3" || second.Entries[0].At != 3 {
		t.Fatalf("second entries = %+v", second.Entries)
	}
	if second.HasMore || second.NextCursor != "" {
		t.Errorf("second meta = %+v", second)
	}
}

func TestMemoryDiaryMirror_EmptyDiary(t *testing.T) {
	h := memoryDiaryMirror(memoryDepsFor(&fakeMemoryStore{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.memory.diary_mirror", map[string]any{}))
	var out diaryMirrorOut
	decode(t, resp, &out)
	if len(out.Entries) != 0 || out.HasMore || out.Total != 0 {
		t.Fatalf("out = %+v", out)
	}
}
