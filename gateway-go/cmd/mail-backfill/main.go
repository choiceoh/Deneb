// mail-backfill seeds the local mailstore from the on-box IMAP archive so the
// mail_archive tool can answer reads without a per-call IMAP round-trip. It is
// idempotent (dedup by Message-ID), so re-running only adds new mail.
//
// Usage:
//
//	mail-backfill                       # backfill all mailboxes into ~/.deneb/mailstore
//	mail-backfill --since 2024-01-01    # only mail sent on/after a date
//	mail-backfill --dir /path/mailstore --batch 500
//
// Archive credentials come from the same env the gateway uses:
// DENEB_ARCHIVE_IMAP_ADDR (default 127.0.0.1:1143), _USER, _PASS, _MAILBOXES.
// The gateway may be running — the store's file appends are atomic and Put is
// idempotent, so a concurrent LMTP write and this backfill don't corrupt shards
// (worst case one message is written by both; dedup collapses it).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mail-backfill:", err)
		os.Exit(1)
	}
}

func run() error {
	defaultDir := filepath.Join(config.ResolveStateDir(), "mailstore")
	dir := flag.String("dir", defaultDir, "mailstore directory")
	sinceStr := flag.String("since", "", "only backfill mail sent on/after this date (YYYY-MM-DD)")
	batch := flag.Int("batch", 200, "IMAP fetch batch size")
	flag.Parse()

	addr := os.Getenv("DENEB_ARCHIVE_IMAP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:1143"
	}
	cfg := mailarchive.Config{
		Addr:      addr,
		User:      os.Getenv("DENEB_ARCHIVE_IMAP_USER"),
		Pass:      os.Getenv("DENEB_ARCHIVE_IMAP_PASS"),
		Mailboxes: mailarchive.ParseMailboxList(os.Getenv("DENEB_ARCHIVE_IMAP_MAILBOXES")),
	}
	if cfg.User == "" || cfg.Pass == "" {
		return fmt.Errorf("아카이브 IMAP 미설정 (DENEB_ARCHIVE_IMAP_USER/PASS)")
	}

	var since time.Time
	if s := *sinceStr; s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return fmt.Errorf("잘못된 --since (YYYY-MM-DD): %w", err)
		}
		since = t
	}

	store, err := mailstore.New(*dir)
	if err != nil {
		return fmt.Errorf("open mailstore: %w", err)
	}
	defer store.Close()
	fmt.Printf("== mail-backfill → %s (기존 %d건) ==\n", *dir, store.Len())

	added, skipped, filtered := 0, 0, 0
	opts := mailarchive.ContextOptions{Mailboxes: cfg.Mailboxes}
	total, ferr := mailarchive.FetchAllContextMessages(context.Background(), cfg, opts, *batch,
		func(m mailarchive.ContextMessage) error {
			if !since.IsZero() && !mailarchive.SentOnOrAfter(m.Date, since) {
				filtered++
				return nil
			}
			created, perr := store.Put(m)
			if perr != nil {
				return perr
			}
			if created {
				added++
			} else {
				skipped++
			}
			if (added+skipped)%1000 == 0 {
				fmt.Printf("  ... %d 처리 (신규 %d, 중복 %d)\n", added+skipped, added, skipped)
			}
			return nil
		})
	if ferr != nil {
		return fmt.Errorf("fetch: %w", ferr)
	}
	fmt.Printf("-- 완료: fetched=%d 신규=%d 중복=%d since-필터=%d 저장소총계=%d --\n",
		total, added, skipped, filtered, store.Len())
	return nil
}
