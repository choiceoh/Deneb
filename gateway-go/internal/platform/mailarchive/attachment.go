package mailarchive

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
)

// ArchivedAttachment is one attachment's raw bytes plus identifying metadata,
// resolved from an archived message. Text extraction (OCR/document parsing) lives
// in the pipeline layer, so this package returns bytes only and never imports it.
type ArchivedAttachment struct {
	Filename string
	MimeType string
	Size     int
	Bytes    []byte
}

// ReadAttachment resolves a message — by archive locator / Message-ID, or by a
// query seed when messageID is empty — and returns its attachments' raw bytes,
// re-parsed from the archived RFC822 source. Because the bytes come from the
// stored raw message it works for every archived mail regardless of the original
// ingest path (LMTP push, IMAP backfill, …); it does not need a live Gmail id the
// way the gmail tool's attachment action does.
//
// selector picks which attachments to return:
//   - ""                          → all attachments that carry bytes
//   - a 1-based index ("1", "2")  → that single attachment
//   - otherwise                   → attachments whose filename contains selector
//     (case-insensitive substring)
//
// Returns ErrArchiveNotFound when the message can't be resolved, and an empty
// slice (no error) when the message has no matching attachment.
func ReadAttachment(ctx context.Context, cfg Config, messageID, query, selector string, opts ContextOptions) ([]ArchivedAttachment, error) {
	if cfg.User == "" || cfg.Pass == "" {
		return nil, fmt.Errorf("archive not configured")
	}
	c, err := connectArchive(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.close()

	seed, err := resolveContextSeed(ctx, c, cfg, messageID, query, opts)
	if err != nil {
		return nil, err
	}
	if seed.Mailbox == "" || seed.UID == "" {
		return nil, ErrArchiveNotFound
	}
	if err := c.examine(seed.Mailbox); err != nil {
		return nil, fmt.Errorf("examine %s: %w", seed.Mailbox, err)
	}
	msgs, err := c.uidFetchMessages(seed.UID)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, ErrArchiveNotFound
	}
	parsed, err := lmtpd.ParseMessage(msgs[0].Raw, archiveLocator(seed.Mailbox, seed.UID))
	if err != nil || parsed == nil || parsed.Detail == nil {
		return nil, fmt.Errorf("parse archived message: %w", err)
	}

	all := make([]ArchivedAttachment, 0, len(parsed.Detail.Attachments))
	for _, att := range parsed.Detail.Attachments {
		b := parsed.AttachmentBytes[att.AttachmentID]
		if len(b) == 0 {
			continue
		}
		all = append(all, ArchivedAttachment{
			Filename: att.Filename,
			MimeType: att.MimeType,
			Size:     len(b),
			Bytes:    b,
		})
	}
	return selectArchivedAttachments(all, selector), nil
}

// selectArchivedAttachments applies the selector documented on ReadAttachment.
func selectArchivedAttachments(atts []ArchivedAttachment, selector string) []ArchivedAttachment {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return atts
	}
	if idx, err := strconv.Atoi(selector); err == nil {
		if idx >= 1 && idx <= len(atts) {
			return []ArchivedAttachment{atts[idx-1]}
		}
		return nil
	}
	lower := strings.ToLower(selector)
	var out []ArchivedAttachment
	for _, a := range atts {
		if strings.Contains(strings.ToLower(a.Filename), lower) {
			out = append(out, a)
		}
	}
	return out
}
