package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestSenderContext_HappyPath(t *testing.T) {
	gmailClient := &fakeGmailClient{
		searchFn: func(_ context.Context, q string, n int) ([]gmail.MessageSummary, error) {
			if q != "from:alice@example.com newer_than:30d" {
				t.Errorf("query = %q", q)
			}
			if n != 50 {
				t.Errorf("limit = %d, want 50", n)
			}
			return []gmail.MessageSummary{
				{ID: "m1", Date: "Mon, 26 May 2026 14:30:00 +0900"},
				{ID: "m2", Date: "Sun, 25 May 2026 09:00:00 +0900"},
			}, nil
		},
	}
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, q string, _ int) ([]wiki.SearchResult, error) {
			if q != "Alice" {
				t.Errorf("wiki query = %q, want Alice (display name)", q)
			}
			return []wiki.SearchResult{
				{Path: "alice.md", Content: "Alice notes", Score: 0.9},
			}, nil
		},
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{
				Meta: wiki.Frontmatter{Title: "Alice", Summary: "Sales contact", Category: "사람"},
			}, nil
		},
	}
	deps := GmailContextDeps{
		Client:    func() (GmailClient, error) { return gmailClient, nil },
		WikiStore: func() (MemorySearcher, error) { return store, nil },
	}
	h := senderContext(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.sender_context", map[string]any{
		"sender": "Alice <alice@example.com>",
	}))

	var got map[string]any
	decode(t, resp, &got)
	if got["email"] != "alice@example.com" || got["displayName"] != "Alice" {
		t.Errorf("parsed sender wrong: %+v", got)
	}
	recent, ok := got["recent"].(map[string]any)
	if !ok {
		t.Fatalf("recent missing/wrong type: %+v", got["recent"])
	}
	if int(recent["count"].(float64)) != 2 || int(recent["windowDays"].(float64)) != 30 {
		t.Errorf("recent fields wrong: %+v", recent)
	}
	hits, _ := got["wikiHits"].([]any)
	if len(hits) != 1 {
		t.Fatalf("wikiHits len = %d, want 1", len(hits))
	}
	hit := hits[0].(map[string]any)
	if hit["title"] != "Alice" || hit["category"] != "사람" {
		t.Errorf("wiki hit metadata wrong: %+v", hit)
	}
}

func TestSenderContext_BareEmail(t *testing.T) {
	var seenGmailQuery string
	var seenWikiQuery string
	deps := GmailContextDeps{
		Client: func() (GmailClient, error) {
			return &fakeGmailClient{
				searchFn: func(_ context.Context, q string, _ int) ([]gmail.MessageSummary, error) {
					seenGmailQuery = q
					return nil, nil
				},
			}, nil
		},
		WikiStore: func() (MemorySearcher, error) {
			return &fakeMemoryStore{
				searchFn: func(_ context.Context, q string, _ int) ([]wiki.SearchResult, error) {
					seenWikiQuery = q
					return nil, nil
				},
			}, nil
		},
	}
	h := senderContext(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.sender_context", map[string]any{
		"sender": "bare@example.com",
	}))

	if !resp.OK {
		t.Fatalf("response error: %+v", resp.Error)
	}
	if seenGmailQuery != "from:bare@example.com newer_than:30d" {
		t.Errorf("gmail query = %q", seenGmailQuery)
	}
	// No display name to query → wiki query falls back to raw input.
	if seenWikiQuery != "bare@example.com" {
		t.Errorf("wiki query = %q, want bare@example.com (fallback)", seenWikiQuery)
	}
}

func TestSenderContext_BareName(t *testing.T) {
	var seenGmailQuery string
	calledGmail := false
	deps := GmailContextDeps{
		Client: func() (GmailClient, error) {
			return &fakeGmailClient{
				searchFn: func(_ context.Context, q string, _ int) ([]gmail.MessageSummary, error) {
					calledGmail = true
					seenGmailQuery = q
					return nil, nil
				},
			}, nil
		},
		WikiStore: func() (MemorySearcher, error) {
			return &fakeMemoryStore{
				searchFn: func(_ context.Context, _ string, _ int) ([]wiki.SearchResult, error) {
					return nil, nil
				},
			}, nil
		},
	}
	h := senderContext(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.sender_context", map[string]any{
		"sender": "Alice",
	}))

	if !resp.OK {
		t.Fatalf("response error: %+v", resp.Error)
	}
	// No email → Gmail search skipped entirely.
	if calledGmail {
		t.Errorf("Gmail search should be skipped for bare-name input, got query=%q", seenGmailQuery)
	}
}

func TestSenderContext_MissingSenderParam(t *testing.T) {
	deps := GmailContextDeps{
		Client: func() (GmailClient, error) { return &fakeGmailClient{}, nil },
	}
	h := senderContext(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.sender_context", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestSenderContext_RequiresAuth(t *testing.T) {
	h := senderContext(GmailContextDeps{
		Client: func() (GmailClient, error) { return &fakeGmailClient{}, nil },
	})
	resp := h(context.Background(), reqWith(t, "miniapp.gmail.sender_context", map[string]any{
		"sender": "a@b.com",
	}))
	if resp.OK {
		t.Fatalf("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestSenderContext_GmailDownStillReturnsWiki(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, _ string, _ int) ([]wiki.SearchResult, error) {
			return []wiki.SearchResult{{Path: "alice.md"}}, nil
		},
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Title: "Alice"}}, nil
		},
	}
	deps := GmailContextDeps{
		Client:    func() (GmailClient, error) { return nil, errors.New("OAuth missing") },
		WikiStore: func() (MemorySearcher, error) { return store, nil },
	}
	h := senderContext(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.sender_context", map[string]any{
		"sender": "Alice <alice@x.com>",
	}))

	var got map[string]any
	decode(t, resp, &got)
	if got["recent"] != nil {
		t.Errorf("recent should be nil when Gmail unavailable, got %+v", got["recent"])
	}
	hits, _ := got["wikiHits"].([]any)
	if len(hits) != 1 {
		t.Errorf("wiki hits should still be returned: %+v", got)
	}
	notices, _ := got["notices"].([]any)
	if len(notices) == 0 {
		t.Errorf("expected a notice about gmail being unavailable")
	}
}

func TestSenderContext_WikiDownStillReturnsRecent(t *testing.T) {
	gmailClient := &fakeGmailClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return []gmail.MessageSummary{{ID: "m1", Date: "Mon, 26 May 2026 14:30:00 +0900"}}, nil
		},
	}
	deps := GmailContextDeps{
		Client:    func() (GmailClient, error) { return gmailClient, nil },
		WikiStore: func() (MemorySearcher, error) { return nil, errors.New("wiki disabled") },
	}
	h := senderContext(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.sender_context", map[string]any{
		"sender": "alice@x.com",
	}))

	var got map[string]any
	decode(t, resp, &got)
	if got["recent"] == nil {
		t.Errorf("recent should be present when Gmail works: %+v", got)
	}
	hits, _ := got["wikiHits"].([]any)
	if len(hits) != 0 {
		t.Errorf("wiki hits should be empty: %+v", hits)
	}
}

func TestGmailContextMethods_NoSourcesReturnsNil(t *testing.T) {
	if got := GmailContextMethods(GmailContextDeps{}); got != nil {
		t.Errorf("expected nil when no sources wired, got %v", got)
	}
}

func TestParseSender(t *testing.T) {
	cases := []struct {
		in              string
		wantEmail       string
		wantDisplayName string
	}{
		{`Alice <alice@example.com>`, "alice@example.com", "Alice"},
		{`"Alice Park" <alice@x.com>`, "alice@x.com", "Alice Park"},
		{`alice@x.com`, "alice@x.com", ""},
		{`Alice`, "", "Alice"},
		{`<noaddr>`, "", "Alice"}, // bogus; sanity-check no crash
	}
	for _, c := range cases {
		got1, got2 := parseSender(c.in)
		if c.in == "<noaddr>" {
			// just don't panic
			continue
		}
		if got1 != c.wantEmail || got2 != c.wantDisplayName {
			t.Errorf("parseSender(%q) = (%q, %q), want (%q, %q)",
				c.in, got1, got2, c.wantEmail, c.wantDisplayName)
		}
	}
}
