package handlerminiapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func searchDepsFor(store MemorySearcher, client PeopleClient) SearchDeps {
	deps := SearchDeps{}
	if store != nil {
		deps.Store = func() (MemorySearcher, error) { return store, nil }
	}
	if client != nil {
		deps.Client = func() (PeopleClient, error) { return client, nil }
	}
	return deps
}

func TestSearchMethods_BothNilReturnsNil(t *testing.T) {
	if got := SearchMethods(SearchDeps{}); got != nil {
		t.Errorf("SearchMethods(empty) = %v, want nil", got)
	}
}

func TestSearchAll_FansOutToAllDomains(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, q string, _ int) ([]wiki.SearchResult, error) {
			if q != "peter" {
				t.Errorf("wiki query = %q", q)
			}
			return []wiki.SearchResult{
				{Path: "people/peter.md", Content: "Peter notes.", Score: 0.9},
			}, nil
		},
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Title: "Peter", Summary: "ops lead", Category: "people"}}, nil
		},
		searchDiaryFn: func(_ context.Context, q string, _ int) ([]wiki.DiaryHit, error) {
			if q != "peter" {
				t.Errorf("diary query = %q", q)
			}
			return []wiki.DiaryHit{
				{File: "diary-2026-05-26.md", Header: "14:30", Content: "met peter", Snippet: "met **peter**", At: 1700000000, Score: 0.75},
			}, nil
		},
	}
	now := time.Date(2026, 5, 26, 15, 0, 0, 0, time.UTC)
	client := &fakePeopleClient{
		searchFn: func(_ context.Context, q string, _ int) ([]gmail.MessageSummary, error) {
			if q != "newer_than:30d -from:me" {
				t.Errorf("gmail query = %q", q)
			}
			return []gmail.MessageSummary{
				{From: "Peter Park <peter@example.com>", Subject: "hi", Date: now.Format(time.RFC3339)},
				{From: "Other <other@example.com>", Subject: "x", Date: now.Format(time.RFC3339)},
			}, nil
		},
	}

	h := searchAll(searchDepsFor(store, client))
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "peter"}))

	var got SearchAllResult
	decode(t, resp, &got)
	if len(got.Wiki) != 1 || got.Wiki[0].Title != "Peter" {
		t.Errorf("wiki = %+v, want Peter card", got.Wiki)
	}
	if len(got.Diary) != 1 || got.Diary[0].File != "diary-2026-05-26.md" {
		t.Errorf("diary = %+v", got.Diary)
	}
	if len(got.Diary) > 0 && got.Diary[0].Content != "met **peter**" {
		t.Errorf("diary content = %q, want snippet-preferred", got.Diary[0].Content)
	}
	if len(got.People) != 1 || got.People[0].Email != "peter@example.com" {
		t.Errorf("people = %+v, want only peter@example.com", got.People)
	}
}

func TestSearchAll_GmailMissingDegrades(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn:      func(_ context.Context, _ string, _ int) ([]wiki.SearchResult, error) { return nil, nil },
		searchDiaryFn: func(_ context.Context, _ string, _ int) ([]wiki.DiaryHit, error) { return nil, nil },
	}
	h := searchAll(searchDepsFor(store, nil))
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "x"}))
	var got SearchAllResult
	decode(t, resp, &got)
	if got.People == nil {
		t.Error("People should be non-nil empty slice, got nil")
	}
	if len(got.People) != 0 {
		t.Errorf("People should be empty when client absent, got %+v", got.People)
	}
}

func TestSearchAll_PerDomainFailureNonFatal(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, _ string, _ int) ([]wiki.SearchResult, error) {
			return nil, errors.New("wiki index unavailable")
		},
		searchDiaryFn: func(_ context.Context, _ string, _ int) ([]wiki.DiaryHit, error) {
			return []wiki.DiaryHit{{File: "d.md", Header: "10:00", Content: "x"}}, nil
		},
	}
	h := searchAll(searchDepsFor(store, nil))
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "x"}))
	var got SearchAllResult
	decode(t, resp, &got)
	if len(got.Wiki) != 0 {
		t.Errorf("wiki should degrade to empty on error, got %+v", got.Wiki)
	}
	if len(got.Diary) != 1 {
		t.Errorf("diary should succeed independently, got %+v", got.Diary)
	}
}

func TestSearchAll_MissingQuery(t *testing.T) {
	h := searchAll(searchDepsFor(&fakeMemoryStore{}, nil))
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{}))
	if resp.Error == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestMatchesPerson(t *testing.T) {
	p := PersonRow{Email: "peter@example.com", Name: "Peter Park"}
	cases := []struct {
		needle string
		want   bool
	}{
		{"peter", true},
		{"PARK", true},
		{"example.com", true},
		{"alice", false},
		{"", true},
	}
	for _, c := range cases {
		if got := matchesPerson(p, c.needle); got != c.want {
			t.Errorf("matchesPerson(%q) = %v, want %v", c.needle, got, c.want)
		}
	}
}
