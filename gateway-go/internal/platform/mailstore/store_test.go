package mailstore_test

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
)

func mkMsg(id, mid, subject, body, date string) mailarchive.ContextMessage {
	return mailarchive.ContextMessage{
		ID:        id,
		Locator:   "INBOX:" + id,
		Mailbox:   "INBOX",
		UID:       id,
		MessageID: mid,
		From:      "sender@example.com",
		Subject:   subject,
		Body:      body,
		Date:      date,
	}
}

// TestPutIdempotentSearchReadReload: Put is idempotent by Message-ID; search,
// read-by-{locator,message-id,id}, and a fresh reload all resolve the message.
func TestPutIdempotentSearchReadReload(t *testing.T) {
	dir := t.TempDir()
	s, err := mailstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := mkMsg("1", "<mid1@x>", "금호타이어 곡성 견적 요청", "곡성 1단계 태양광 모듈 견적", "Mon, 02 Jun 2026 10:00:00 +0900")

	created, err := s.Put(m)
	if err != nil || !created {
		t.Fatalf("first Put: created=%v err=%v", created, err)
	}
	if again, _ := s.Put(m); again {
		t.Fatal("duplicate Put must be idempotent (created=false)")
	}
	if s.Len() != 1 {
		t.Fatalf("Len=%d, want 1", s.Len())
	}

	if hits := s.Search(nil, "곡성 견적", time.Time{}, 10); len(hits) != 1 {
		t.Fatalf("Search=%d, want 1", len(hits))
	}
	for _, ref := range []string{"INBOX:1", "<mid1@x>", "1"} {
		if _, ok := s.Read(ref, "", nil); !ok {
			t.Errorf("Read(%q) not found", ref)
		}
	}

	// Reload from disk — the JSONL shard rebuilds the same index.
	s2, err := mailstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Len() != 1 {
		t.Fatalf("reload Len=%d, want 1", s2.Len())
	}
	if hits := s2.Search(nil, "곡성", time.Time{}, 10); len(hits) != 1 {
		t.Fatalf("reload Search=%d, want 1", len(hits))
	}
}

// TestProjectHistoryAndThread: history ranks both messages of a project; thread
// walks the References edge from the seed.
func TestProjectHistoryAndThread(t *testing.T) {
	dir := t.TempDir()
	s, err := mailstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(mkMsg("1", "<a@x>", "금호 곡성 1차 견적", "견적 요청드립니다", "Mon, 02 Jun 2026 10:00:00 +0900")); err != nil {
		t.Fatal(err)
	}
	m2 := mkMsg("2", "<b@x>", "RE: 금호 곡성 1차 견적", "회신드립니다", "Tue, 03 Jun 2026 10:00:00 +0900")
	m2.References = []string{"<a@x>"}
	if _, err := s.Put(m2); err != nil {
		t.Fatal(err)
	}

	hist, ok := s.ProjectHistory("금호 곡성 견적", time.Time{}, 10, 0)
	if !ok || len(hist.Messages) != 2 {
		t.Fatalf("ProjectHistory ok=%v n=%d, want 2", ok, len(hist.Messages))
	}
	if len(hist.Threads) == 0 {
		t.Error("ProjectHistory produced no thread clusters")
	}

	thread, ok := s.Thread("<a@x>", "", nil, 10)
	if !ok || len(thread) != 2 {
		t.Fatalf("Thread ok=%v n=%d, want 2 (seed + reply via References)", ok, len(thread))
	}
}

// TestMissesReturnEmpty: an empty store or a non-matching query returns nothing
// / ok=false so the tool knows to fall back to IMAP.
func TestMissesReturnEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := mailstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.ProjectHistory("아무거나", time.Time{}, 10, 0); ok {
		t.Error("empty store ProjectHistory must be ok=false")
	}
	if _, ok := s.Read("없는-id", "", nil); ok {
		t.Error("missing Read must be ok=false")
	}
	if _, ok := s.Thread("없는-id", "", nil, 10); ok {
		t.Error("missing Thread must be ok=false")
	}
	if hits := s.Search(nil, "없는키워드zzz", time.Time{}, 10); len(hits) != 0 {
		t.Errorf("no-match Search=%d, want 0", len(hits))
	}
}

// TestSinceFilter_ReturnsOnlyMessagesOnOrAfterSince: List/Search honor the since bound (KST calendar day).
func TestSinceFilter_ReturnsOnlyMessagesOnOrAfterSince(t *testing.T) {
	dir := t.TempDir()
	s, err := mailstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Put(mkMsg("old", "<old@x>", "예전 곡성 메일", "본문", "Mon, 02 Jun 2026 10:00:00 +0900"))
	s.Put(mkMsg("new", "<new@x>", "최근 곡성 메일", "본문", "Wed, 02 Jul 2026 10:00:00 +0900"))

	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if got := s.List(nil, since, 10); len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("List since=%v got %d msgs, want just 'new'", since, len(got))
	}
	if got := s.Search(nil, "곡성", since, 10); len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("Search since got %d, want just 'new'", len(got))
	}
}
