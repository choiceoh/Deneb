package wiki

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newForgetTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestForgetRemovesPageAndRecordsTombstone(t *testing.T) {
	store := newForgetTestStore(t)
	page := NewPage("홍길동", "인물", nil)
	page.Body = "전화번호 010-0000-0000"
	if err := store.WritePage("인물/홍길동.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	res, err := store.Forget("인물/홍길동", "사용자가 개인정보 삭제 요청")
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if res.Title != "홍길동" || res.Path != "인물/홍길동.md" {
		t.Fatalf("unexpected result: %+v", res)
	}

	// Page is gone from disk.
	if _, err := os.Stat(filepath.Join(store.dir, "인물/홍길동.md")); !os.IsNotExist(err) {
		t.Fatalf("page still on disk after forget")
	}
	// Page is gone from read + search (no longer surfaces in recall).
	if _, err := store.ReadPage("인물/홍길동.md"); err == nil {
		t.Fatalf("ReadPage should fail after forget")
	}
	results, _ := store.Search(context.Background(), "홍길동", 10)
	for _, r := range results {
		if strings.Contains(r.Path, "홍길동") {
			t.Fatalf("forgotten page still in search results: %+v", results)
		}
	}

	// Audit tombstone with the reason persists in log.md.
	logBody, err := os.ReadFile(filepath.Join(store.dir, "log.md"))
	if err != nil {
		t.Fatalf("read log.md: %v", err)
	}
	if !strings.Contains(string(logBody), "forget") ||
		!strings.Contains(string(logBody), "개인정보 삭제 요청") {
		t.Fatalf("tombstone/reason missing from audit log:\n%s", logBody)
	}
}

func TestForgetFlattensMultilineReasonInTombstone(t *testing.T) {
	store := newForgetTestStore(t)
	page := NewPage("멀티라인", "기타", nil)
	if err := store.WritePage("기타/멀티라인.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	// A multi-line reason attempting to inject a fake log heading.
	if _, err := store.Forget("기타/멀티라인", "진짜 사유\n## [2000-01-01 00:00] 가짜op\n스푸핑"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	logBody, err := os.ReadFile(filepath.Join(store.dir, "log.md"))
	if err != nil {
		t.Fatalf("read log.md: %v", err)
	}
	// The forget entry's reason line must be single-line: no injected heading.
	for _, line := range strings.Split(string(logBody), "\n") {
		if strings.Contains(line, "가짜op") && strings.HasPrefix(strings.TrimSpace(line), "##") {
			t.Fatalf("multi-line reason injected a fake heading:\n%s", logBody)
		}
	}
	if !strings.Contains(string(logBody), "진짜 사유 ## [2000-01-01 00:00] 가짜op 스푸핑") {
		t.Fatalf("reason not flattened to one line:\n%s", logBody)
	}
}

func TestForgetDropsSemanticVectorSynchronously(t *testing.T) {
	store := newForgetTestStore(t)
	store.SetEmbedder(fakeEmbedder{healthy: true})
	page := NewPage("잊을거", "기타", nil)
	page.Body = "semantic recall 대상 본문"
	if err := store.WritePage("기타/잊을거.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	// Warm the index so the page's vector is live in s.sem.vecs.
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("WarmSemanticIndex: %v", err)
	}
	store.sem.mu.Lock()
	_, present := store.sem.vecs["기타/잊을거.md"]
	store.sem.mu.Unlock()
	if !present {
		t.Fatalf("precondition: vector should be present after warm")
	}

	if _, err := store.Forget("기타/잊을거", "프라이버시"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	// The vector must be gone immediately — not left for an async refresh that
	// Search would race against (privacy contract).
	store.sem.mu.Lock()
	_, stillThere := store.sem.vecs["기타/잊을거.md"]
	store.sem.mu.Unlock()
	if stillThere {
		t.Fatalf("forgotten page's semantic vector still present")
	}
}

func TestForgetSemanticRemovalSurvivesReload(t *testing.T) {
	store := newForgetTestStore(t)
	store.SetEmbedder(fakeEmbedder{healthy: true})
	page := NewPage("재시작후에도", "기타", nil)
	page.Body = "복원되면 안 되는 본문"
	if err := store.WritePage("기타/재시작후에도.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("WarmSemanticIndex: %v", err) // also persists the on-disk cache
	}

	if _, err := store.Forget("기타/재시작후에도", "프라이버시"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	// Simulate a gateway restart: re-attach the embedder, which reloads the
	// on-disk semantic cache. The forgotten vector must NOT come back.
	store.SetEmbedder(fakeEmbedder{healthy: true})
	store.sem.mu.Lock()
	_, back := store.sem.vecs["기타/재시작후에도.md"]
	store.sem.mu.Unlock()
	if back {
		t.Fatalf("forgotten vector reloaded from cache after restart")
	}
}

func TestForgetRequiresReason(t *testing.T) {
	store := newForgetTestStore(t)
	page := NewPage("x", "기타", nil)
	if err := store.WritePage("기타/x.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if _, err := store.Forget("기타/x", "  "); err == nil {
		t.Fatalf("Forget should require a reason")
	}
	// Fail-closed: the page must still exist when the reason is missing.
	if _, err := store.ReadPage("기타/x.md"); err != nil {
		t.Fatalf("page was removed despite missing reason: %v", err)
	}
}

func TestForgetMissingPageErrors(t *testing.T) {
	store := newForgetTestStore(t)
	if _, err := store.Forget("없음/page", "reason"); err == nil {
		t.Fatalf("Forget on a missing page should error")
	}
}
