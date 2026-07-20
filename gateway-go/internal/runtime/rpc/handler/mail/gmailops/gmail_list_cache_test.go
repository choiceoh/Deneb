package gmailops

import (
	"context"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// --- listCache unit behavior ---------------------------------------------

func TestListCache_HitWithinTTL(t *testing.T) {
	c := newListCache(30 * time.Second)
	base := time.Unix(1000, 0)
	c.put("k", map[string]any{"v": 1}, base)

	got, ok := c.get("k", base.Add(10*time.Second))
	if !ok || got["v"] != 1 {
		t.Fatalf("want hit with v=1, got ok=%v val=%v", ok, got)
	}
}

func TestListCacheExpiresJustPastTTLWithInclusiveBoundary(t *testing.T) {
	c := newListCache(30 * time.Second)
	base := time.Unix(1000, 0)
	c.put("k", map[string]any{"v": 1}, base)

	if _, ok := c.get("k", base.Add(31*time.Second)); ok {
		t.Fatal("want miss past TTL")
	}
	// Exactly at TTL is still a hit (boundary is inclusive).
	if _, ok := c.get("k", base.Add(30*time.Second)); !ok {
		t.Fatal("want hit exactly at TTL boundary")
	}
}

func TestListCache_MissUnknownKey(t *testing.T) {
	c := newListCache(30 * time.Second)
	if _, ok := c.get("nope", time.Unix(1000, 0)); ok {
		t.Fatal("want miss for unknown key")
	}
}

func TestListCache_Invalidate(t *testing.T) {
	c := newListCache(30 * time.Second)
	base := time.Unix(1000, 0)
	c.put("a", map[string]any{"v": 1}, base)
	c.put("b", map[string]any{"v": 2}, base)
	c.invalidate()

	if _, ok := c.get("a", base); ok {
		t.Error("key a survived invalidate")
	}
	if _, ok := c.get("b", base); ok {
		t.Error("key b survived invalidate")
	}
}

func TestListCache_NilSafe(t *testing.T) {
	var c *listCache // nil — caching disabled
	c.put("k", map[string]any{"v": 1}, time.Unix(1000, 0))
	if _, ok := c.get("k", time.Unix(1000, 0)); ok {
		t.Fatal("nil cache must always miss")
	}
	c.invalidate() // must not panic
}

// --- handler caching behavior --------------------------------------------

// inboxStub returns a fakeGmailClient whose list call increments *calls
// and whose label/trash mutations succeed, so cache invalidation can be
// observed purely through the list-call count.
func inboxStub(calls *int, labels ...string) *fakeGmailClient {
	if len(labels) == 0 {
		labels = []string{"INBOX"}
	}
	return &fakeGmailClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			*calls++
			return []gmail.MessageSummary{{ID: "m1", Labels: labels}}, nil
		},
		modifyLabelsFn: func(_ context.Context, _ string, _, _ []string) error { return nil },
		trashFn:        func(_ context.Context, _ string) error { return nil },
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1", Labels: []string{"INBOX"}}, nil
		},
	}
}

func TestGmailListRecent_CacheHitWithinTTL(t *testing.T) {
	var calls int
	client := inboxStub(&calls)
	h := gmailListRecent(depsFor(client), newListCache(30*time.Second))

	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	for i := 0; i < 3; i++ {
		resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
		decode(t, resp, &got)
		if len(got.Messages) != 1 {
			t.Fatalf("call %d: len(messages)=%d, want 1", i, len(got.Messages))
		}
	}
	if calls != 1 {
		t.Errorf("list fetched %d times across 3 identical requests, want 1 (cached)", calls)
	}
}

func TestGmailListRecentCachesEachQuerySeparatelyAndSkipsReloadOnRepeat(t *testing.T) {
	var calls int
	client := inboxStub(&calls)
	cache := newListCache(30 * time.Second)
	h := gmailListRecent(depsFor(client), cache)

	h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", map[string]any{"query": "is:starred"}))
	h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", map[string]any{"query": "is:important"}))
	// Repeat the first — should hit cache, not re-fetch.
	h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", map[string]any{"query": "is:starred"}))

	if calls != 2 {
		t.Errorf("list fetched %d times, want 2 (two distinct queries, third cached)", calls)
	}
}

func TestGmailListRecent_ArchiveInvalidatesCache(t *testing.T) {
	var calls int
	client := inboxStub(&calls)
	cache := newListCache(30 * time.Second)
	listH := gmailListRecent(depsFor(client), cache)
	archiveH := gmailArchive(depsFor(client), cache)

	listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil)) // fetch + cache
	resp := archiveH(authedCtx(), reqWith(t, "miniapp.gmail.archive", map[string]any{"id": "m1"}))
	if !resp.OK {
		t.Fatalf("archive failed: %+v", resp.Error)
	}
	listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil)) // must re-fetch

	if calls != 2 {
		t.Errorf("list fetched %d times, want 2 (archive must invalidate)", calls)
	}
}

func TestGmailListRecent_TrashInvalidatesCache(t *testing.T) {
	var calls int
	client := inboxStub(&calls)
	cache := newListCache(30 * time.Second)
	listH := gmailListRecent(depsFor(client), cache)
	trashH := gmailTrash(depsFor(client), cache)

	listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	resp := trashH(authedCtx(), reqWith(t, "miniapp.gmail.trash", map[string]any{"id": "m1"}))
	if !resp.OK {
		t.Fatalf("trash failed: %+v", resp.Error)
	}
	listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))

	if calls != 2 {
		t.Errorf("list fetched %d times, want 2 (trash must invalidate)", calls)
	}
}

// TestGmailListRecent_MarkReadKeepsCache pins the deliberate decision that
// mark_read does NOT invalidate: the Mini App fires it automatically on
// every mail open, so invalidating there would blow the cache away on each
// tap. Inbox membership is unchanged by marking read, so the cached list
// stays valid.
func TestGmailListRecent_MarkReadKeepsCache(t *testing.T) {
	var calls int
	client := inboxStub(&calls, "INBOX", "UNREAD")
	cache := newListCache(30 * time.Second)
	listH := gmailListRecent(depsFor(client), cache)
	markH := gmailMarkRead(depsFor(client)) // shares no cache by design

	listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	resp := markH(authedCtx(), reqWith(t, "miniapp.gmail.mark_read", map[string]any{"id": "m1"}))
	if !resp.OK {
		t.Fatalf("mark_read failed: %+v", resp.Error)
	}
	listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))

	if calls != 1 {
		t.Errorf("list fetched %d times, want 1 (mark_read must NOT invalidate)", calls)
	}
}

func TestGmailListRecent_StaleRefreshDoesNotRestoreAfterArchive(t *testing.T) {
	var calls int
	releaseFetch := make(chan struct{})
	client := &fakeGmailClient{
		searchPageFn: func(_ context.Context, _, _ string, _ int) ([]gmail.MessageSummary, string, error) {
			calls++
			if calls > 1 {
				<-releaseFetch
			}
			return []gmail.MessageSummary{{ID: "m1", Labels: []string{"INBOX"}}}, "", nil
		},
		modifyLabelsFn: func(_ context.Context, _ string, _, _ []string) error { return nil },
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1", Labels: []string{"INBOX"}}, nil
		},
	}
	cache := newListCache(30 * time.Second)
	listH := gmailListRecent(depsFor(client), cache)
	archiveH := gmailArchive(depsFor(client), cache)

	// Seed a stale entry (past TTL, within ceiling).
	now := time.Now()
	cache.put("{in:inbox is:unread} newer_than:30d|60||0", map[string]any{
		"messages": []mailRowOut{{ID: "m1"}},
	}, now.Add(-2*time.Minute))

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp := listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
		if !resp.OK {
			t.Errorf("stale list failed: %+v", resp.Error)
		}
	}()

	// Wait for the blocked background refresh to start.
	deadline := time.Now().Add(2 * time.Second)
	for calls < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls < 1 {
		t.Fatal("background stale refresh never started")
	}

	resp := archiveH(authedCtx(), reqWith(t, "miniapp.gmail.archive", map[string]any{"id": "m1"}))
	if !resp.OK {
		t.Fatalf("archive failed: %+v", resp.Error)
	}

	close(releaseFetch)
	<-done

	listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))

	if calls != 2 {
		t.Fatalf("list fetched %d times, want 2 (stale refresh must not resurrect cache after archive)", calls)
	}
}

func TestListCache_StaleServeAndSingleFlightRefresh(t *testing.T) {
	c := newListCache(30 * time.Second)
	now := time.Now()
	c.put("k", map[string]any{"v": 1}, now)

	// Fresh window: not stale (the fresh get path owns it).
	if _, ok, _, _ := c.getStale("k", now.Add(10*time.Second)); ok {
		t.Fatal("fresh entry must not be served via the stale path")
	}

	// Past TTL, within ceiling: served, and the first caller wins the refresh.
	at := now.Add(2 * time.Minute)
	payload, ok, refresh, gen := c.getStale("k", at)
	if !ok || payload["v"] != 1 || !refresh || gen != 0 {
		t.Fatalf("want stale hit + refresh claim, got ok=%v refresh=%v gen=%d", ok, refresh, gen)
	}
	if _, ok, refresh, _ := c.getStale("k", at); !ok || refresh {
		t.Fatalf("second caller must be served without a duplicate refresh claim (ok=%v refresh=%v)", ok, refresh)
	}
	c.refreshDone("k")
	if _, ok, refresh, _ := c.getStale("k", at); !ok || !refresh {
		t.Fatalf("after refreshDone the claim must be available again (ok=%v refresh=%v)", ok, refresh)
	}

	// Past the ceiling: dead.
	if _, ok, _, _ := c.getStale("k", now.Add(listCacheStaleServeCeiling+time.Second)); ok {
		t.Fatal("entry past the stale ceiling must not be served")
	}

	// Nil safety mirrors the other methods.
	var nilCache *listCache
	if _, ok, _, _ := nilCache.getStale("k", at); ok {
		t.Fatal("nil cache must miss")
	}
	nilCache.refreshDone("k")
}

func TestGmailListRecent_SyncFetchDoesNotRestoreAfterArchive(t *testing.T) {
	var calls int
	releaseFetch := make(chan struct{})
	client := &fakeGmailClient{
		searchPageFn: func(_ context.Context, _, _ string, _ int) ([]gmail.MessageSummary, string, error) {
			calls++
			if calls > 1 {
				<-releaseFetch
			}
			return []gmail.MessageSummary{{ID: "m1", Labels: []string{"INBOX"}}}, "", nil
		},
		modifyLabelsFn: func(_ context.Context, _ string, _, _ []string) error { return nil },
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1", Labels: []string{"INBOX"}}, nil
		},
	}
	cache := newListCache(30 * time.Second)
	listH := gmailListRecent(depsFor(client), cache)
	archiveH := gmailArchive(depsFor(client), cache)

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp := listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
		if !resp.OK {
			t.Errorf("sync list failed: %+v", resp.Error)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for calls < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls < 1 {
		t.Fatal("sync fetch never started")
	}

	resp := archiveH(authedCtx(), reqWith(t, "miniapp.gmail.archive", map[string]any{"id": "m1"}))
	if !resp.OK {
		t.Fatalf("archive failed: %+v", resp.Error)
	}

	close(releaseFetch)
	<-done

	listH(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))

	if calls != 2 {
		t.Fatalf("list fetched %d times, want 2 (sync fetch must not resurrect cache after archive)", calls)
	}
}

func TestListCache_PutIfGenerationSkipsAfterInvalidate(t *testing.T) {
	c := newListCache(30 * time.Second)
	now := time.Now()
	c.put("k", map[string]any{"v": 1}, now.Add(-2*time.Minute))

	_, ok, refresh, gen := c.getStale("k", now)
	if !ok || !refresh {
		t.Fatalf("want stale refresh claim, got ok=%v refresh=%v", ok, refresh)
	}

	c.invalidate()

	c.putIfGeneration("k", map[string]any{"v": 2}, now, gen)
	if _, hit := c.get("k", now); hit {
		t.Fatal("stale background put must not restore cache after invalidate")
	}
}

func TestListCache_InvalidateClearsInFlightRefreshClaim(t *testing.T) {
	c := newListCache(30 * time.Second)
	now := time.Now()
	c.put("k", map[string]any{"v": 1}, now.Add(-2*time.Minute))

	_, ok, refresh, _ := c.getStale("k", now)
	if !ok || !refresh {
		t.Fatal("want refresh claim")
	}

	c.invalidate()

	_, ok, refresh, _ = c.getStale("k", now.Add(-2*time.Minute))
	if ok || refresh {
		t.Fatalf("after invalidate a new stale refresh must not be blocked (ok=%v refresh=%v)", ok, refresh)
	}
}
