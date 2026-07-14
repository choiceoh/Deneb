// mail-backfill seeds the local mailstore so the mail_archive tool (and the app
// mail get action) read without a per-call round-trip. Two sources:
//
//	--source imap   (default) — the on-box IMAP archive (LMTP-received mail)
//	--source gmail            — Gmail API, newest N messages (--limit, default 5000)
//
// Both are idempotent (dedup by Message-ID), so re-running only adds new mail.
//
// Usage:
//
//	mail-backfill                              # all of the on-box IMAP archive
//	mail-backfill --source gmail --limit 5000  # newest 5000 Gmail messages
//	mail-backfill --source gmail --query "in:anywhere" --since 2024-01-01
//
// IMAP creds: DENEB_ARCHIVE_IMAP_ADDR/USER/PASS/MAILBOXES. Gmail creds:
// ~/.deneb/credentials (same OAuth token the mailanalysis service uses).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
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
	source := flag.String("source", "imap", "백필 소스: imap(온박스 아카이브) | gmail(Gmail API)")
	sinceStr := flag.String("since", "", "이 날짜 이후만 백필 (YYYY-MM-DD)")
	batch := flag.Int("batch", 200, "IMAP fetch 배치 크기 (imap 소스)")
	query := flag.String("query", "", "Gmail 검색 쿼리 (gmail 소스, 기본 전체)")
	limit := flag.Int("limit", 5000, "Gmail 최대 건수 (gmail 소스)")
	flag.Parse()

	since, err := parseSince(*sinceStr)
	if err != nil {
		return err
	}
	if err := validateSource(*source); err != nil {
		return err
	}

	store, err := mailstore.New(*dir)
	if err != nil {
		return fmt.Errorf("open mailstore: %w", err)
	}
	defer store.Close()
	fmt.Printf("== mail-backfill (%s) → %s (기존 %d건) ==\n", *source, *dir, store.Len())

	switch *source {
	case "gmail":
		return backfillGmail(store, *query, *limit, since)
	case "imap", "":
		return backfillIMAP(store, *batch, since)
	default:
		return fmt.Errorf("알 수 없는 --source: %s (imap | gmail)", *source)
	}
}

func parseSince(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("잘못된 --since (YYYY-MM-DD): %w", err)
	}
	return t, nil
}

func validateSource(source string) error {
	switch source {
	case "gmail", "imap", "":
		return nil
	default:
		return fmt.Errorf("알 수 없는 --source: %s (imap | gmail)", source)
	}
}

// backfillIMAP walks the on-box IMAP archive into the store.
func backfillIMAP(store *mailstore.Store, batch int, since time.Time) error {
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

	added, skipped, filtered := 0, 0, 0
	opts := mailarchive.ContextOptions{Mailboxes: cfg.Mailboxes}
	total, ferr := mailarchive.FetchAllContextMessages(context.Background(), cfg, opts, batch,
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
	fmt.Printf("-- IMAP 완료: fetched=%d 신규=%d 중복=%d since-필터=%d 저장소총계=%d --\n",
		total, added, skipped, filtered, store.Len())
	return nil
}

// backfillGmail imports the newest `limit` Gmail messages into the store via the
// Gmail API, paginating 500 at a time. Each summary is expanded to a full detail
// (body + headers, incl. the Message-ID now parsed by messageToDetail) so dedup
// matches the LMTP path. Best-effort per message: a fetch error skips that one.
func backfillGmail(store *mailstore.Store, query string, limit int, since time.Time) error {
	gc, err := gmail.DefaultClient()
	if err != nil {
		return fmt.Errorf("gmail client (~/.deneb/credentials): %w", err)
	}
	ctx := context.Background()
	added, skipped, filtered, fetched := 0, 0, 0, 0
	pageToken := ""
	for fetched < limit {
		pageSize := 500
		if r := limit - fetched; r < pageSize {
			pageSize = r
		}
		summaries, next, serr := gc.SearchPage(ctx, query, pageToken, pageSize)
		if serr != nil {
			return fmt.Errorf("gmail search (fetched=%d): %w", fetched, serr)
		}
		if len(summaries) == 0 {
			break
		}
		for i := range summaries {
			fetched++
			detail, derr := gc.GetMessage(ctx, summaries[i].ID)
			if derr != nil || detail == nil {
				continue // best-effort — skip a message we can't fetch
			}
			cm := mailarchive.ContextMessageFromDetail("Gmail", summaries[i].ID, detail, 0)
			if !since.IsZero() && !mailarchive.SentOnOrAfter(cm.Date, since) {
				filtered++
				continue
			}
			created, perr := store.Put(cm)
			if perr != nil {
				return perr
			}
			if created {
				added++
			} else {
				skipped++
			}
			if (added+skipped)%500 == 0 {
				fmt.Printf("  ... %d 처리 (신규 %d, 중복 %d)\n", added+skipped, added, skipped)
			}
		}
		if next == "" {
			break
		}
		pageToken = next
	}
	fmt.Printf("-- Gmail 완료: fetched=%d 신규=%d 중복=%d since-필터=%d 저장소총계=%d --\n",
		fetched, added, skipped, filtered, store.Len())
	return nil
}
