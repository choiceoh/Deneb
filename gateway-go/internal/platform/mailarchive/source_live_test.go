package mailarchive

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
)

// TestRelatedMessages_Live derives a real sender + Message-ID from the archive,
// constructs a synthetic incoming reply, and verifies the source finds the
// referenced message and/or sender history. Gated like TestIMAP_Live.
func TestRelatedMessages_Live(t *testing.T) {
	if os.Getenv("DENEB_ARCHIVE_IMAP_LIVE") == "" {
		t.Skip("set DENEB_ARCHIVE_IMAP_LIVE=1 to run against a live archive IMAP")
	}
	addr := envOr("DENEB_ARCHIVE_IMAP_ADDR", "127.0.0.1:1143")
	user := os.Getenv("DENEB_ARCHIVE_IMAP_USER")
	pass := os.Getenv("DENEB_ARCHIVE_IMAP_PASS")

	// Grab a sample archived message to derive a real sender + Message-ID.
	c, err := dialIMAP(context.Background(), addr, 10*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.login(user, pass); err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := c.examine("Gmail"); err != nil {
		t.Fatalf("examine: %v", err)
	}
	uids, err := c.uidSearch("SINCE 01-Jan-2020")
	if err != nil || len(uids) == 0 {
		t.Fatalf("search: %v (n=%d)", err, len(uids))
	}
	bodies, err := c.uidFetchBodies(uids[len(uids)-1]) // most recent
	c.logout()
	c.close()
	if err != nil || len(bodies) == 0 {
		t.Fatalf("fetch sample: %v", err)
	}
	sample, err := lmtpd.ParseDetail(bodies[0])
	if err != nil {
		t.Fatalf("parse sample: %v", err)
	}
	t.Logf("sample: from=%q msgid=%q subj=%q", sample.From, sample.MessageIDHeader, sample.Subject)

	src := New(Config{Addr: addr, User: user, Pass: pass, Mailboxes: []string{"INBOX", "Gmail"}})
	incoming := &gmail.MessageDetail{
		From:            sample.From,
		Subject:         "Re: " + sample.Subject,
		MessageIDHeader: "<synthetic-incoming-test@deneb.local>",
		References:      []string{sample.MessageIDHeader},
	}
	rel, err := src.RelatedMessages(context.Background(), incoming)
	if err != nil {
		t.Fatalf("RelatedMessages: %v", err)
	}
	t.Logf("related messages found: %d", len(rel))
	if len(rel) == 0 {
		t.Fatal("expected related messages (sender history at minimum)")
	}
	foundSample := false
	for _, r := range rel {
		if sample.MessageIDHeader != "" && normalizeMsgID(r.MessageIDHeader) == normalizeMsgID(sample.MessageIDHeader) {
			foundSample = true
		}
	}
	t.Logf("referenced sample recovered via thread/sender lookup: %v", foundSample)
}
