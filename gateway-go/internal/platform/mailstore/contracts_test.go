package mailstore

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

func contractMessage(id, mailbox, subject, body, date string) mailarchive.ContextMessage {
	return mailarchive.ContextMessage{
		ID:        id,
		Locator:   mailbox + ":" + id,
		Mailbox:   mailbox,
		UID:       id,
		MessageID: "<" + id + "@example.test>",
		From:      "sender@example.test",
		To:        "recipient@example.test",
		Subject:   subject,
		Body:      body,
		Date:      date,
	}
}

func newContractStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func putContractMessages(t *testing.T, s *Store, msgs ...mailarchive.ContextMessage) {
	t.Helper()
	for _, msg := range msgs {
		created, err := s.Put(msg)
		if err != nil || !created {
			t.Fatalf("Put(%s): created=%v err=%v", msg.ID, created, err)
		}
	}
}

func messageIDs(msgs []mailarchive.ContextMessage) []string {
	out := make([]string, len(msgs))
	for i := range msgs {
		out[i] = msgs[i].ID
	}
	return out
}

func TestMatchMailboxContract(t *testing.T) {
	msg := contractMessage("1", "INBOX", "subject", "body", "Mon, 02 Jun 2026 10:00:00 +0900")
	for _, tt := range []struct {
		name      string
		mailboxes []string
		want      bool
	}{
		{name: "nil", want: true},
		{name: "empty", mailboxes: []string{}, want: true},
		{name: "exact", mailboxes: []string{"INBOX"}, want: true},
		{name: "case insensitive", mailboxes: []string{"inbox"}, want: true},
		{name: "filter whitespace", mailboxes: []string{"  inbox  "}, want: true},
		{name: "message whitespace", mailboxes: []string{"INBOX"}, want: true},
		{name: "second match", mailboxes: []string{"Archive", "INBOX"}, want: true},
		{name: "missing", mailboxes: []string{"Sent"}, want: false},
		{name: "blank filter", mailboxes: []string{""}, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := msg
			if tt.name == "message whitespace" {
				candidate.Mailbox = " INBOX "
			}
			if got := matchMailbox(candidate, tt.mailboxes); got != tt.want {
				t.Fatalf("matchMailbox = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClipContract(t *testing.T) {
	msgs := []mailarchive.ContextMessage{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	for _, tt := range []struct {
		name  string
		limit int
		want  []string
	}{
		{name: "negative unlimited", limit: -1, want: []string{"1", "2", "3"}},
		{name: "zero unlimited", limit: 0, want: []string{"1", "2", "3"}},
		{name: "one", limit: 1, want: []string{"1"}},
		{name: "exact", limit: 3, want: []string{"1", "2", "3"}},
		{name: "large", limit: 9, want: []string{"1", "2", "3"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := messageIDs(clip(msgs, tt.limit)); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("clip = %v, want %v", got, tt.want)
			}
		})
	}
	if got := clip(nil, 1); got != nil {
		t.Fatalf("nil clip = %#v", got)
	}
}

func TestShardPathContract(t *testing.T) {
	s := &Store{dir: "/state/mail"}
	for _, tt := range []struct {
		name string
		date string
		want string
	}{
		{name: "rfc5322", date: "Mon, 02 Jun 2026 10:00:00 +0900", want: "2026-06.jsonl"},
		{name: "iso date", date: "2026-12-31", want: "2026-12.jsonl"},
		{name: "rfc1123", date: "Thu, 02 Jan 2025 03:04:05 +0000", want: "2025-01.jsonl"},
		{name: "empty", want: "unknown.jsonl"},
		{name: "invalid", date: "next someday", want: "unknown.jsonl"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := filepath.Base(s.shardPath(mailarchive.ContextMessage{Date: tt.date})); got != tt.want {
				t.Fatalf("shard = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPutRejectsMessagesWithoutDedupeKey(t *testing.T) {
	s := newContractStore(t)
	created, err := s.Put(mailarchive.ContextMessage{})
	if created || err == nil || !strings.Contains(err.Error(), "no dedupe key") {
		t.Fatalf("Put empty = %v, %v", created, err)
	}
	if s.Len() != 0 {
		t.Fatalf("Len = %d", s.Len())
	}
}

func TestNewRejectsNonDirectoryRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "regular-file")
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root); err == nil || !strings.Contains(err.Error(), "mkdir") {
		t.Fatalf("New error = %v", err)
	}
}

func TestNew_IgnoresNonJSONLAndCorruptShards(t *testing.T) {
	root := t.TempDir()
	msgDir := filepath.Join(root, "messages")
	if err := os.MkdirAll(filepath.Join(msgDir, "nested.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(msgDir, "notes.txt"), []byte(`{"id":"ignored"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(msgDir, "broken.jsonl"), []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	valid := contractMessage("valid", "INBOX", "survives", "content", "Mon, 02 Jun 2026 10:00:00 +0900")
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(msgDir, "valid.jsonl"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}
	if got, ok := s.Read("valid", "", nil); !ok || got.Subject != "survives" {
		t.Fatalf("Read = %+v/%v", got, ok)
	}
}

func TestPutPersistsOneJSONRecordAndReloadsIndexes(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	msg := contractMessage("record", "Archive", "persistent alpha", "persistent beta", "Thu, 20 Aug 2026 10:30:00 +0000")
	putContractMessages(t, s, msg)
	if again, err := s.Put(msg); err != nil || again {
		t.Fatalf("duplicate = %v/%v", again, err)
	}
	path := filepath.Join(root, "messages", "2026-08.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("record lines = %d, want 1", len(lines))
	}
	reloaded, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, locator := range []string{"record", "Archive:record", "<record@example.test>", "record@example.test"} {
		got, ok := reloaded.Read(locator, "", nil)
		if !ok || got.ID != "record" {
			t.Errorf("Read(%q) = %+v/%v", locator, got, ok)
		}
	}
	if got := reloaded.Search(nil, "persistent beta", time.Time{}, 5); len(got) != 1 || got[0].ID != "record" {
		t.Fatalf("Search = %v", messageIDs(got))
	}
}

func TestPutAppendFailureDoesNotMutateMemory(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "messages")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "messages"), []byte("blocks directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err := s.Put(contractMessage("fail", "INBOX", "subject", "body", "2026-01-02"))
	if created || err == nil || !strings.Contains(err.Error(), "append") {
		t.Fatalf("Put = %v/%v", created, err)
	}
	if s.Len() != 0 {
		t.Fatalf("failed Put mutated Len to %d", s.Len())
	}
}

func TestPutConcurrentDuplicateContract(t *testing.T) {
	s := newContractStore(t)
	msg := contractMessage("same", "INBOX", "concurrent", "delivery", "2026-04-01")
	const workers = 48
	var wg sync.WaitGroup
	created := make(chan bool, workers)
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.Put(msg)
			created <- ok
			errs <- err
		}()
	}
	wg.Wait()
	close(created)
	close(errs)
	createdCount := 0
	for ok := range created {
		if ok {
			createdCount++
		}
	}
	for err := range errs {
		if err != nil {
			t.Errorf("Put error: %v", err)
		}
	}
	if createdCount != 1 || s.Len() != 1 {
		t.Fatalf("created=%d Len=%d", createdCount, s.Len())
	}
}

func TestList_ReturnsNewestFirstFilteredByMailboxAndSince(t *testing.T) {
	s := newContractStore(t)
	putContractMessages(
		t, s,
		contractMessage("old-inbox", "INBOX", "alpha", "one", "Thu, 01 Jan 2026 01:00:00 +0000"),
		contractMessage("new-inbox", "INBOX", "alpha", "two", "Sun, 01 Mar 2026 01:00:00 +0000"),
		contractMessage("middle-sent", "Sent", "alpha", "three", "Sun, 01 Feb 2026 01:00:00 +0000"),
		contractMessage("unknown", "INBOX", "alpha", "four", "not-a-date"),
	)
	if got := messageIDs(s.List(nil, time.Time{}, 0)); !reflect.DeepEqual(got[:3], []string{"new-inbox", "middle-sent", "old-inbox"}) {
		t.Fatalf("newest order = %v", got)
	}
	if got := messageIDs(s.List([]string{" inbox "}, time.Time{}, 2)); !reflect.DeepEqual(got, []string{"new-inbox", "old-inbox"}) {
		t.Fatalf("mailbox/limit = %v", got)
	}
	since := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if got := messageIDs(s.List(nil, since, -1)); !reflect.DeepEqual(got, []string{"new-inbox", "middle-sent", "unknown"}) {
		t.Fatalf("since = %v", got)
	}
}

func TestSearchContractAndStoredScoreIsolation(t *testing.T) {
	s := newContractStore(t)
	first := contractMessage("first", "INBOX", "orion launch orion", "critical sequence", "2026-01-01")
	first.Score = 7
	second := contractMessage("second", "Archive", "orion", "routine note", "2026-02-01")
	third := contractMessage("third", "INBOX", "unrelated", "nothing", "2026-03-01")
	putContractMessages(t, s, first, second, third)
	for _, blank := range []string{"", " ", "\n\t"} {
		if got := s.Search(nil, blank, time.Time{}, 10); got != nil {
			t.Errorf("blank Search(%q) = %#v", blank, got)
		}
	}
	hits := s.Search(nil, "orion", time.Time{}, 10)
	if len(hits) != 2 || hits[0].ID != "first" {
		t.Fatalf("Search order = %v", messageIDs(hits))
	}
	if hits[0].Score <= 7 {
		t.Fatalf("search score not combined: %f", hits[0].Score)
	}
	stored, ok := s.Read("first", "", nil)
	if !ok || stored.Score != 7 {
		t.Fatalf("stored score mutated: %+v/%v", stored, ok)
	}
	if got := messageIDs(s.Search([]string{"archive"}, "orion", time.Time{}, 10)); !reflect.DeepEqual(got, []string{"second"}) {
		t.Fatalf("mailbox Search = %v", got)
	}
	if got := s.Search(nil, "orion", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 1); len(got) != 1 || got[0].ID != "second" {
		t.Fatalf("since Search = %v", messageIDs(got))
	}
	if got := s.Search(nil, "orion", time.Time{}, 1); len(got) != 1 {
		t.Fatalf("limit Search = %d", len(got))
	}
}

func TestReadLocatorMessageIDBareIDQueryAndFilters(t *testing.T) {
	s := newContractStore(t)
	msg := contractMessage("lookup", "Archive", "zephyr project", "blueprint", "2026-01-01")
	putContractMessages(t, s, msg)
	for _, input := range []string{"Archive:lookup", "<lookup@example.test>", "lookup@example.test", "lookup", "  lookup  "} {
		got, ok := s.Read(input, "", []string{"archive"})
		if !ok || got.ID != "lookup" {
			t.Errorf("Read(%q) = %+v/%v", input, got, ok)
		}
	}
	for _, input := range []string{"Archive:lookup", "<lookup@example.test>", "lookup"} {
		if got, ok := s.Read(input, "", []string{"INBOX"}); ok || got.ID != "" {
			t.Errorf("wrong-mailbox Read(%q) = %+v/%v", input, got, ok)
		}
	}
	if got, ok := s.Read("", "  zephyr blueprint ", []string{"Archive"}); !ok || got.ID != "lookup" {
		t.Fatalf("query Read = %+v/%v", got, ok)
	}
	for _, miss := range []struct{ id, query string }{{}, {"missing", ""}, {"", "missing term"}, {"missing", "missing term"}} {
		if _, ok := s.Read(miss.id, miss.query, nil); ok {
			t.Errorf("Read(%q,%q) unexpectedly matched", miss.id, miss.query)
		}
	}
}

func TestThreadTransitiveFixpointChronologyAndMailboxIsolation(t *testing.T) {
	s := newContractStore(t)
	root := contractMessage("root", "INBOX", "thread", "root", "2026-01-01")
	child := contractMessage("child", "INBOX", "thread", "child", "2026-01-02")
	child.References = []string{"root@example.test"}
	grandchild := contractMessage("grandchild", "INBOX", "thread", "grandchild", "2026-01-03")
	grandchild.References = []string{"<child@example.test>"}
	parent := contractMessage("parent", "INBOX", "thread", "parent", "2025-12-31")
	root.References = []string{"<parent@example.test>"}
	foreign := contractMessage("foreign", "Sent", "thread", "foreign", "2026-01-04")
	foreign.References = []string{"<grandchild@example.test>"}
	unrelated := contractMessage("unrelated", "INBOX", "other", "other", "2026-01-05")
	putContractMessages(t, s, grandchild, foreign, unrelated, root, parent, child)
	got, ok := s.Thread("root", "", []string{"INBOX"}, 40)
	if !ok {
		t.Fatal("Thread not found")
	}
	if ids := messageIDs(got); !reflect.DeepEqual(ids, []string{"parent", "root", "child", "grandchild"}) {
		t.Fatalf("Thread = %v", ids)
	}
	if _, ok := s.Thread("foreign", "", []string{"INBOX"}, 40); ok {
		t.Fatal("wrong-mailbox seed matched")
	}
	if got, ok := s.Thread("", "grandchild", []string{"INBOX"}, 2); !ok || !reflect.DeepEqual(messageIDs(got), []string{"child", "grandchild"}) {
		t.Fatalf("limited Thread = %v/%v", messageIDs(got), ok)
	}
}

func TestThreadLimitKeepsNewestAfterCompleteGraphWalk(t *testing.T) {
	s := newContractStore(t)
	const count = 15
	msgs := make([]mailarchive.ContextMessage, 0, count)
	for i := 0; i < count; i++ {
		id := "node-" + time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC).Format("02")
		msg := contractMessage(id, "INBOX", "chain", "message", time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339))
		if i > 0 {
			msg.References = []string{"<" + msgs[i-1].ID + "@example.test>"}
		}
		msgs = append(msgs, msg)
	}
	// Reverse insertion makes map traversal unable to rely on append order.
	for i := len(msgs) - 1; i >= 0; i-- {
		putContractMessages(t, s, msgs[i])
	}
	got, ok := s.Thread(msgs[0].ID, "", nil, 4)
	if !ok {
		t.Fatal("Thread not found")
	}
	want := []string{msgs[11].ID, msgs[12].ID, msgs[13].ID, msgs[14].ID}
	if ids := messageIDs(got); !reflect.DeepEqual(ids, want) {
		t.Fatalf("limited chain = %v, want %v", ids, want)
	}
}

func TestThreadDefaultLimitAndDedupe(t *testing.T) {
	s := newContractStore(t)
	root := contractMessage("root", "INBOX", "topic", "root", "2026-01-01")
	putContractMessages(t, s, root)
	for i := 1; i <= 45; i++ {
		id := time.Date(2026, 2, i, 0, 0, 0, 0, time.UTC).Format("20060102")
		msg := contractMessage(id, "INBOX", "topic", "reply", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i)*time.Hour).Format(time.RFC3339))
		msg.References = []string{"<root@example.test>", "<root@example.test>"}
		putContractMessages(t, s, msg)
	}
	got, ok := s.Thread("root", "", nil, 0)
	if !ok || len(got) != 40 {
		t.Fatalf("default Thread = %d/%v", len(got), ok)
	}
	seen := map[string]bool{}
	for _, msg := range got {
		if seen[msg.ID] {
			t.Fatalf("duplicate %q", msg.ID)
		}
		seen[msg.ID] = true
	}
}

func TestProjectHistoryContracts(t *testing.T) {
	s := newContractStore(t)
	if got, ok := s.ProjectHistory("project", time.Time{}, 10, 10); ok || len(got.Messages) != 0 {
		t.Fatalf("empty history = %+v/%v", got, ok)
	}
	putContractMessages(
		t, s,
		contractMessage("one", "INBOX", "apollo project", "launch scope", "2026-01-01"),
		contractMessage("two", "Sent", "RE: apollo project", "launch answer", "2026-02-01"),
		contractMessage("three", "INBOX", "different", "unrelated", "2026-03-01"),
	)
	if got, ok := s.ProjectHistory("no-such-term", time.Time{}, 10, 10); ok || len(got.Messages) != 0 {
		t.Fatalf("miss history = %+v/%v", got, ok)
	}
	history, ok := s.ProjectHistory("apollo launch", time.Time{}, 1, 0)
	if !ok || !history.IndexUsed || history.Query != "apollo launch" || len(history.Messages) != 1 {
		t.Fatalf("history = %+v/%v", history, ok)
	}
	if history.Messages[0].Score <= 0 {
		t.Fatalf("history score = %f", history.Messages[0].Score)
	}
	if got, ok := s.ProjectHistory("apollo", time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), 10, 10); !ok || len(got.Messages) != 1 || got.Messages[0].ID != "two" {
		t.Fatalf("since/index history = %v/%v", messageIDs(got.Messages), ok)
	}
}

func TestPutInMemoryLockedIgnoresEmptyAndIndexesOptionalKeys(t *testing.T) {
	s := &Store{
		idx:     nil,
		byKey:   make(map[string]mailarchive.ContextMessage),
		byLoc:   make(map[string]string),
		byMsgID: make(map[string]string),
		byID:    make(map[string]string),
	}
	// The empty-key return precedes index use, so a partially constructed store
	// can safely ignore an unusable record during a defensive load path.
	s.putInMemoryLocked(mailarchive.ContextMessage{})
	if len(s.byKey) != 0 {
		t.Fatalf("empty message indexed: %#v", s.byKey)
	}
	s.idx = newContractStore(t).idx
	msg := contractMessage("minimal", "INBOX", "indexed", "body", "2026-01-01")
	msg.Locator = ""
	msg.MessageID = ""
	s.putInMemoryLocked(msg)
	if len(s.byKey) != 1 || len(s.byID) != 1 || len(s.byLoc) != 0 || len(s.byMsgID) != 0 {
		t.Fatalf("maps: key=%d id=%d loc=%d msgid=%d", len(s.byKey), len(s.byID), len(s.byLoc), len(s.byMsgID))
	}
}

func TestConcurrentReadersAndWritersContract(t *testing.T) {
	s := newContractStore(t)
	const writers = 24
	const readers = 12
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC).Format("20060102")
			_, _ = s.Put(contractMessage(id, "INBOX", "parallel nebula", "body", time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)))
		}()
	}
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_ = s.Len()
				_ = s.List(nil, time.Time{}, 5)
				_ = s.Search(nil, "nebula", time.Time{}, 5)
				_, _ = s.Read("missing", "nebula", nil)
			}
		}()
	}
	wg.Wait()
	if s.Len() != writers {
		t.Fatalf("Len = %d, want %d", s.Len(), writers)
	}
	got := messageIDs(s.List(nil, time.Time{}, 0))
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	if len(sorted) != writers {
		t.Fatalf("List = %d", len(sorted))
	}
}

func TestCloseIsIdempotentNoOp(t *testing.T) {
	s := newContractStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	putContractMessages(t, s, contractMessage("after-close", "INBOX", "still open", "body", "2026-01-01"))
	if s.Len() != 1 {
		t.Fatalf("Len = %d", s.Len())
	}
}
