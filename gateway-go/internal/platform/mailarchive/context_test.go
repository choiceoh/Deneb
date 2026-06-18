package mailarchive

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestThreadContextReconstructsArchiveThreadAcrossMailboxes(t *testing.T) {
	root := archiveContextTestMessage("root@deneb", "alpha@example.com", "Alpha project decision", "Wed, 17 Jun 2026 09:00:00 +0000", "Root decision.", "", "")
	parent := archiveContextTestMessage("parent@deneb", "beta@example.com", "Re: Alpha project decision", "Wed, 17 Jun 2026 10:00:00 +0000", "Parent follow-up.", "<root@deneb>", "<root@deneb>")
	reply := archiveContextTestMessage("reply@deneb", "gamma@example.com", "Re: Alpha project decision", "Wed, 17 Jun 2026 11:00:00 +0000", "Latest reply.", "<root@deneb> <parent@deneb>", "<parent@deneb>")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"Gmail": {"1": []byte(root), "2": []byte(parent)},
		"INBOX": {"3": []byte(reply)},
	})
	cfg := Config{Addr: srv.addr, User: "u", Pass: "p", Mailboxes: []string{"INBOX", "Gmail"}, Timeout: time.Second}

	msgs, err := ThreadContext(context.Background(), cfg, archiveLocator("INBOX", "3"), "", ContextOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ThreadContext: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len=%d want 3: %#v", len(msgs), msgs)
	}
	gotIDs := []string{msgs[0].MessageID, msgs[1].MessageID, msgs[2].MessageID}
	wantIDs := []string{"<root@deneb>", "<parent@deneb>", "<reply@deneb>"}
	if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("thread order=%v want %v", gotIDs, wantIDs)
	}
	if msgs[0].Mailbox != "Gmail" || msgs[2].Mailbox != "INBOX" {
		t.Fatalf("mailboxes=%s/%s want Gmail/INBOX", msgs[0].Mailbox, msgs[2].Mailbox)
	}
	if msgs[2].Locator != archiveLocator("INBOX", "3") {
		t.Fatalf("reply locator=%q", msgs[2].Locator)
	}
}

func TestThreadContextFallsBackToSubjectParticipantsWhenHeadersAreMissing(t *testing.T) {
	older := archiveContextTestMessage("alpha-fallback-old@deneb", "alpha@example.com", "Alpha fallback decision", "Wed, 17 Jun 2026 09:00:00 +0000", "Original decision.", "", "")
	reply := archiveContextTestMessage("alpha-fallback-reply@deneb", "alpha@example.com", "Re: Alpha fallback decision", "Wed, 17 Jun 2026 11:00:00 +0000", "Reply without References.", "", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"Gmail": {"1": []byte(older)},
		"INBOX": {"2": []byte(reply)},
	})
	cfg := Config{Addr: srv.addr, User: "u", Pass: "p", Mailboxes: []string{"INBOX", "Gmail"}, Timeout: time.Second}

	msgs, err := ThreadContext(context.Background(), cfg, archiveLocator("INBOX", "2"), "", ContextOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ThreadContext: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len=%d want 2: %#v", len(msgs), msgs)
	}
	if msgs[0].MessageID != "<alpha-fallback-old@deneb>" || msgs[1].MessageID != "<alpha-fallback-reply@deneb>" {
		t.Fatalf("fallback thread order=%s -> %s", msgs[0].MessageID, msgs[1].MessageID)
	}
}

func TestProjectHistoryContextBuildsTimelineAndThreadClusters(t *testing.T) {
	older := archiveContextTestMessage("alpha-old@deneb", "alpha@example.com", "Alpha project estimate", "Wed, 17 Jun 2026 09:00:00 +0000", "Alpha budget history.", "", "")
	newer := archiveContextTestMessage("alpha-new@deneb", "boss@example.com", "Re: Alpha project estimate", "Wed, 17 Jun 2026 12:00:00 +0000", "Alpha approved.", "<alpha-old@deneb>", "<alpha-old@deneb>")
	other := archiveContextTestMessage("beta@deneb", "beta@example.com", "Beta project", "Wed, 17 Jun 2026 13:00:00 +0000", "No match.", "", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"Gmail": {"1": []byte(older), "3": []byte(other)},
		"INBOX": {"2": []byte(newer)},
	})
	cfg := Config{Addr: srv.addr, User: "u", Pass: "p", Mailboxes: []string{"INBOX", "Gmail"}, Timeout: time.Second}

	history, err := ProjectHistoryContext(context.Background(), cfg, "Alpha", ContextOptions{Limit: 20})
	if err != nil {
		t.Fatalf("ProjectHistoryContext: %v", err)
	}
	if len(history.Messages) != 2 {
		t.Fatalf("messages=%d want 2: %#v", len(history.Messages), history.Messages)
	}
	if history.Messages[0].MessageID != "<alpha-old@deneb>" || history.Messages[1].MessageID != "<alpha-new@deneb>" {
		t.Fatalf("timeline order=%s -> %s", history.Messages[0].MessageID, history.Messages[1].MessageID)
	}
	if len(history.Threads) != 1 {
		t.Fatalf("threads=%d want 1: %#v", len(history.Threads), history.Threads)
	}
	if history.Threads[0].Count != 2 || !strings.Contains(strings.ToLower(history.Threads[0].Subject), "alpha project estimate") {
		t.Fatalf("thread cluster=%#v", history.Threads[0])
	}
	if len(history.Threads[0].Locators) == 0 {
		t.Fatalf("thread cluster should expose representative locators")
	}
	if !history.IndexUsed {
		t.Fatalf("project history should use local FTS index")
	}
}

func TestProjectHistoryRanksBusinessSignalsBeforePlainMatches(t *testing.T) {
	plain := archiveContextTestMessage("solar-plain@deneb", "plain@example.com", "SolarPrime update", "Wed, 17 Jun 2026 13:00:00 +0000", "SolarPrime 참고 메일입니다.", "", "")
	important := archiveContextTestMessage("solar-important@deneb", "sales@example.com", "SolarPrime update", "Wed, 17 Jun 2026 09:00:00 +0000", "SolarPrime 견적 금액 100만원, 납기 마감 2026-06-30 검토 필요.", "", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": {"1": []byte(plain), "2": []byte(important)},
	})
	cfg := Config{Addr: srv.addr, User: "u", Pass: "p", Mailboxes: []string{"INBOX"}, Timeout: time.Second}

	history, err := ProjectHistoryContext(context.Background(), cfg, "SolarPrime", ContextOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ProjectHistoryContext: %v", err)
	}
	if len(history.Messages) != 1 {
		t.Fatalf("messages=%d want 1: %#v", len(history.Messages), history.Messages)
	}
	if history.Messages[0].MessageID != "<solar-important@deneb>" {
		t.Fatalf("ranked message=%s want important", history.Messages[0].MessageID)
	}
	reasons := strings.Join(history.Messages[0].RankReasons, ",")
	if !strings.Contains(reasons, "deadline_or_action") || !strings.Contains(reasons, "money") {
		t.Fatalf("rank reasons=%v want deadline and money", history.Messages[0].RankReasons)
	}
}

func TestSearchContextMessagesReturnsNewestFirst(t *testing.T) {
	older := archiveContextTestMessage("alpha-old@deneb", "alpha@example.com", "Alpha update", "Wed, 17 Jun 2026 09:00:00 +0000", "Alpha older.", "", "")
	newer := archiveContextTestMessage("alpha-new@deneb", "alpha@example.com", "Alpha update", "Wed, 17 Jun 2026 12:00:00 +0000", "Alpha newer.", "", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": {"1": []byte(older), "2": []byte(newer)},
	})
	cfg := Config{Addr: srv.addr, User: "u", Pass: "p", Mailboxes: []string{"INBOX"}, Timeout: time.Second}

	msgs, err := SearchContextMessages(context.Background(), cfg, "Alpha", ContextOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchContextMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages=%d want 2: %#v", len(msgs), msgs)
	}
	if msgs[0].MessageID != "<alpha-new@deneb>" || msgs[1].MessageID != "<alpha-old@deneb>" {
		t.Fatalf("search order=%s -> %s, want newest first", msgs[0].MessageID, msgs[1].MessageID)
	}
}

func archiveContextTestMessage(messageID, from, subject, date, body, references, inReplyTo string) string {
	var extra strings.Builder
	if references != "" {
		fmt.Fprintf(&extra, "References: %s\r\n", references)
	}
	if inReplyTo != "" {
		fmt.Fprintf(&extra, "In-Reply-To: %s\r\n", inReplyTo)
	}
	return fmt.Sprintf("From: Sender <%s>\r\nTo: User <user@example.com>\r\nSubject: %s\r\nMessage-ID: <%s>\r\nDate: %s\r\n%sContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		from, subject, messageID, date, extra.String(), body)
}
