// mail_autobackfill.go — one-shot startup seeding of the local mail store from
// the on-box IMAP archive.
//
// The local mailstore (#3270) only receives NEW mail at runtime (LMTP intake +
// gmail-poll). Historical mail must be seeded, and that used to be a manual
// cmd/mail-backfill run only — so on a fresh store, mail_archive reads of older
// mail silently fell back to the ~12.9s per-call IMAP path (see the phase-timing
// log path=imap-fallback). This seeds the whole archive once in the background so
// those reads hit the fast local path instead.
//
// One-shot via a sentinel file: it runs exactly once (not every boot), and
// independent of Len() — the store already holds recent mail, so an "empty store"
// guard would never fire on a live host. store.Put dedups by Message-ID, so the
// pass is idempotent and overlaps safely with live LMTP/gmail-poll writes.
package server

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	// mailAutoBackfillMarker names the sentinel (under the mailstore dir) written
	// after a completed full pass so later boots skip it.
	mailAutoBackfillMarker = ".autobackfill-done"
	// mailAutoBackfillBatch is the IMAP fetch batch size (same default as cmd/mail-backfill).
	mailAutoBackfillBatch = 200
	// mailAutoBackfillTimeout bounds the background pass so a wedged IMAP fetch
	// can't run forever; derived from ShutdownCtx so shutdown also cancels it.
	mailAutoBackfillTimeout = 30 * time.Minute
)

// autoBackfillEnabled reports whether the one-shot mail backfill should run: on
// by default unless DENEB_MAIL_AUTOBACKFILL=0, archive IMAP creds must be set,
// and no completed-pass marker may exist yet. Pure (marker path + creds in) so
// the decision is unit-testable.
func autoBackfillEnabled(markerPath, imapUser, imapPass string) bool {
	if os.Getenv("DENEB_MAIL_AUTOBACKFILL") == "0" {
		return false
	}
	if imapUser == "" || imapPass == "" {
		return false // nothing to backfill from
	}
	if _, err := os.Stat(markerPath); err == nil {
		return false // a full pass already completed
	}
	return true
}

// maybeAutoBackfillMailStore seeds the local mail store from the on-box IMAP
// archive in the background, once, when it hasn't been done yet. Best-effort and
// fully detached: on failure it leaves reads on the IMAP fallback (today's
// behavior) and retries next boot (no marker written), never a user-facing error.
func (s *Server) maybeAutoBackfillMailStore(mailStoreDir string) {
	if s.mailStore == nil || mailStoreDir == "" {
		return
	}
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
	markerPath := filepath.Join(mailStoreDir, mailAutoBackfillMarker)
	if !autoBackfillEnabled(markerPath, cfg.User, cfg.Pass) {
		return
	}

	store := s.mailStore
	logger := s.logger
	safego.GoWithSlog(logger, "mail-autobackfill", func() {
		ctx, cancel := context.WithTimeout(s.ShutdownCtx(), mailAutoBackfillTimeout)
		defer cancel()

		start := time.Now()
		added := 0
		opts := mailarchive.ContextOptions{Mailboxes: cfg.Mailboxes}
		total, err := mailarchive.FetchAllContextMessages(ctx, cfg, opts, mailAutoBackfillBatch,
			func(m mailarchive.ContextMessage) error {
				created, perr := store.Put(m)
				if perr != nil {
					return perr
				}
				if created {
					added++
				}
				return nil
			})
		if err != nil {
			// Degraded, not user-facing: reads stay on the IMAP fallback and the
			// pass retries next boot (marker not written). Warn, not Error.
			logger.Warn("mail-autobackfill: incomplete; reads use the IMAP fallback until a later boot completes it",
				"scanned", total, "added", added, "error", err)
			return
		}
		// Mark a completed pass so later boots skip. 0o600: owner-only sentinel.
		werr := os.WriteFile(markerPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600) //nolint:gosec // G703 — marker path is the state-dir mailstore + const name, not user input
		if werr != nil {
			logger.Warn("mail-autobackfill: seed done but marker write failed (may re-run next boot)",
				"path", markerPath, "error", werr)
		}
		logger.Info("mail-autobackfill: seeded local store from archive",
			"scanned", total, "added", added, "duration", time.Since(start).Round(time.Second))
	})
}
