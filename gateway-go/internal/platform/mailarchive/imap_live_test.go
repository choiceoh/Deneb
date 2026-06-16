package mailarchive

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestIMAP_Live exercises the minimal client against a real archive IMAP server.
// Gated: set DENEB_ARCHIVE_IMAP_LIVE=1 plus ADDR/USER/PASS. CI skips it (no server).
func TestIMAP_Live(t *testing.T) {
	if os.Getenv("DENEB_ARCHIVE_IMAP_LIVE") == "" {
		t.Skip("set DENEB_ARCHIVE_IMAP_LIVE=1 to run against a live archive IMAP")
	}
	addr := envOr("DENEB_ARCHIVE_IMAP_ADDR", "127.0.0.1:1143")
	user := os.Getenv("DENEB_ARCHIVE_IMAP_USER")
	pass := os.Getenv("DENEB_ARCHIVE_IMAP_PASS")

	c, err := dialIMAP(context.Background(), addr, 10*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.close()
	if err := c.login(user, pass); err != nil {
		t.Fatalf("login: %v", err)
	}
	defer c.logout()

	if err := c.examine("Gmail"); err != nil {
		t.Fatalf("examine: %v", err)
	}
	// Search a broad criterion that should match plenty in a 1000-message archive.
	uids, err := c.uidSearch(`SINCE 01-Jan-2020`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	t.Logf("Gmail UIDs matched: %d", len(uids))
	if len(uids) == 0 {
		t.Fatal("expected some messages in archive Gmail mailbox")
	}
	// Fetch one body and sanity-check it parses as a mail with headers.
	bodies, err := c.uidFetchBodies(uids[0])
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 body, got %d", len(bodies))
	}
	head := string(bodies[0])
	if len(head) > 200 {
		head = head[:200]
	}
	if !strings.Contains(string(bodies[0]), ":") {
		t.Fatalf("fetched body doesn't look like a message: %q", head)
	}
	t.Logf("fetched %d bytes, head: %q", len(bodies[0]), head)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
