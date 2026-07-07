package mailarchive

// backfill.go — FetchAllContextMessages streams the entire archive (no bounded
// index window) so cmd/mail-backfill can seed the local mailstore. The imapConn
// and fetch helpers are unexported, so this exported seam lives in-package; it
// mirrors searchContextMessagesLimited's per-mailbox examine→uidSearch→fetch
// loop but paginates over ALL UIDs instead of clamping to a recent window.

import (
	"context"
)

// FetchAllContextMessages dials the archive once and invokes cb for every
// message across the configured mailboxes, fetched in batches of batchSize
// (default 200). Returns the number of messages handed to cb. A cb error stops
// the walk and propagates. Per-mailbox errors (examine/search) are skipped so a
// single bad folder doesn't abort the whole backfill.
func FetchAllContextMessages(ctx context.Context, cfg Config, opts ContextOptions, batchSize int, cb func(ContextMessage) error) (int, error) {
	if batchSize <= 0 {
		batchSize = 200
	}
	c, err := connectArchive(ctx, cfg)
	if err != nil {
		return 0, err
	}
	defer c.close()
	defer c.logout()

	total := 0
	for _, mailbox := range contextMailboxes(cfg, opts.Mailboxes) {
		if ctx.Err() != nil {
			return total, ctx.Err()
		}
		if err := c.examine(mailbox); err != nil {
			continue
		}
		uids, err := c.uidSearch("ALL")
		if err != nil {
			continue
		}
		for i := 0; i < len(uids); i += batchSize {
			if ctx.Err() != nil {
				return total, ctx.Err()
			}
			end := i + batchSize
			if end > len(uids) {
				end = len(uids)
			}
			msgs, ferr := fetchContextUIDsAfterExamine(ctx, c, mailbox, uids[i:end], opts)
			if ferr != nil {
				continue // partial batch failure — skip, backfill re-run is idempotent
			}
			for _, msg := range msgs {
				if cberr := cb(msg); cberr != nil {
					return total, cberr
				}
				total++
			}
		}
	}
	return total, nil
}
