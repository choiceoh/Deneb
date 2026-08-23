package mailarchive

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive/overlay"
)

const (
	archivePageTokenPrefix = "archive:"
	archiveLocatorPrefix   = "archive|"
	defaultNativeLookback  = 30 * 24 * time.Hour
	// Search syntax such as has:attachment and the local read/archive overlay
	// are applied after IMAP returns candidates. Scan a wider local window than
	// one page so a list does not look nearly empty just because the newest rows
	// were filtered out.
	minArchiveFetchPerBox           = 300
	maxArchiveFetchPerBox           = 1000
	archivePostFilterScanMultiplier = 6
)

var (
	ErrArchiveUnavailable      = errors.New("mailarchive: archive unavailable")
	ErrArchiveUnsupportedQuery = errors.New("mailarchive: unsupported query")
	ErrArchiveNotFound         = errors.New("mailarchive: message not found")
)

// FallbackClient is the legacy Gmail client surface the native mail repository
// can delegate to when the archive is disabled or a Gmail-only query/token is
// requested.
type FallbackClient interface {
	Search(ctx context.Context, query string, maxResults int) ([]gmail.MessageSummary, error)
	SearchPage(ctx context.Context, query, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error)
	GetMessage(ctx context.Context, messageID string) (*gmail.MessageDetail, error)
	ModifyLabels(ctx context.Context, messageID string, addNames, removeNames []string) error
	Trash(ctx context.Context, messageID string) error
	GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

// RepositoryOptions configures local state and Gmail fallback behavior.
type RepositoryOptions struct {
	StatePath string
	Fallback  FallbackClient
	Now       func() time.Time
}

// NativeStatus summarizes archive availability for the native client.
type NativeStatus struct {
	Source         string
	Available      bool
	OfflineCapable bool
	Mailboxes      []NativeMailboxStatus
	Overlay        NativeOverlayStatus
	GeneratedAt    time.Time
}

// NativeMailboxStatus describes the configured on-box archive mailbox.
type NativeMailboxStatus struct {
	Name              string
	Total             int
	Unread            int
	LocallyRead       int
	LocallyArchived   int
	LocallyTrashed    int
	LatestUID         string
	AttachmentCapable bool
}

// NativeOverlayStatus reports the local read/archive/trash state overlay.
type NativeOverlayStatus struct {
	Messages int
	Read     int
	Archived int
	Trashed  int
}

// Repository exposes the on-box IMAP archive through the Gmail-like interface
// already used by miniapp.mail.*. Reads prefer the local archive; Gmail remains
// a compatibility fallback for disabled archive setups and unsupported legacy
// Gmail search tokens.
type Repository struct {
	cfg      Config
	state    *overlay.Store
	fallback FallbackClient
	now      func() time.Time
}

// NewRepository constructs an archive repository with optional Gmail fallback.
func NewRepository(cfg Config, opts RepositoryOptions) *Repository {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if len(cfg.Mailboxes) == 0 {
		cfg.Mailboxes = DefaultMailboxes()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Repository{
		cfg:      cfg,
		state:    overlay.NewStore(opts.StatePath),
		fallback: opts.Fallback,
		now:      now,
	}
}

// Search returns the first page of messages matching query.
func (r *Repository) Search(ctx context.Context, query string, maxResults int) ([]gmail.MessageSummary, error) {
	rows, _, err := r.SearchPage(ctx, query, "", maxResults)
	return rows, err
}

// SearchPage returns a page of archive messages and the next page token.
func (r *Repository) SearchPage(ctx context.Context, query, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error) {
	if r == nil || !r.archiveEnabled() {
		return r.fallbackSearchPage(ctx, query, pageToken, maxResults)
	}
	if strings.TrimSpace(pageToken) != "" && !strings.HasPrefix(pageToken, archivePageTokenPrefix) {
		return r.fallbackSearchPage(ctx, query, pageToken, maxResults)
	}
	spec := parseArchiveQuery(query, r.now())
	if spec.Degraded != "" {
		slog.Warn("mailarchive: query degraded to recent view", "query", query, "reason", spec.Degraded)
	}
	rows, next, err := r.searchArchive(ctx, spec, pageToken, maxResults)
	if err != nil {
		if r.fallback != nil {
			return r.fallback.SearchPage(ctx, query, pageToken, maxResults)
		}
		// No Gmail fallback anymore — surface the failure to the operator so a wedged
		// archive (IMAP down) doesn't just look like an empty inbox to the user.
		slog.Warn("mailarchive: search failed (native-only)", "query", query, "error", err)
		return nil, "", err
	}
	return rows, next, nil
}

// GetMessage resolves a full message from the archive or configured fallback.
func (r *Repository) GetMessage(ctx context.Context, messageID string) (*gmail.MessageDetail, error) {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil, ErrArchiveNotFound
	}
	if r != nil && r.archiveEnabled() {
		msg, err := r.getArchiveParsed(ctx, messageID)
		if err == nil && msg != nil {
			detail := cloneDetail(msg.Detail)
			r.applyStateToDetail(detail, r.state.Get(messageID))
			return detail, nil
		}
		if r.fallback == nil || !errors.Is(err, ErrArchiveNotFound) {
			return nil, err
		}
	}
	if r != nil && r.fallback != nil {
		return r.fallback.GetMessage(ctx, messageID)
	}
	return nil, ErrArchiveUnavailable
}

// ModifyLabels applies supported native label changes to messageID.
func (r *Repository) ModifyLabels(ctx context.Context, messageID string, addNames, removeNames []string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ErrArchiveNotFound
	}
	if r != nil && r.archiveEnabled() && r.canMutateArchiveMessage(ctx, messageID) {
		return r.applyArchiveLabelMutation(messageID, removeNames)
	}
	if r != nil && r.fallback != nil {
		return r.fallback.ModifyLabels(ctx, messageID, addNames, removeNames)
	}
	return ErrArchiveUnavailable
}

// Trash marks messageID trashed in the native state overlay.
func (r *Repository) Trash(ctx context.Context, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return ErrArchiveNotFound
	}
	if r != nil && r.archiveEnabled() && r.canMutateArchiveMessage(ctx, messageID) {
		return r.state.MarkTrashed(messageID)
	}
	if r != nil && r.fallback != nil {
		return r.fallback.Trash(ctx, messageID)
	}
	return ErrArchiveUnavailable
}

// GetAttachment retrieves one message attachment from the backing source.
func (r *Repository) GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error) {
	messageID = strings.TrimSpace(messageID)
	attachmentID = strings.TrimSpace(attachmentID)
	if messageID == "" || attachmentID == "" {
		return nil, ErrArchiveNotFound
	}
	if r != nil && r.archiveEnabled() {
		msg, err := r.getArchiveParsed(ctx, messageID)
		if err == nil && msg != nil {
			if data, ok := msg.AttachmentBytes[attachmentID]; ok {
				return data, nil
			}
			if r.fallback == nil {
				return nil, ErrArchiveNotFound
			}
		} else if r.fallback == nil || !errors.Is(err, ErrArchiveNotFound) {
			return nil, err
		}
	}
	if r != nil && r.fallback != nil {
		return r.fallback.GetAttachment(ctx, messageID, attachmentID)
	}
	return nil, ErrArchiveUnavailable
}

// NativeStatus reports archive connectivity and local overlay counts.
func (r *Repository) NativeStatus(ctx context.Context) (NativeStatus, error) {
	if r == nil || !r.archiveEnabled() {
		// Native-archive-only now (Gmail fallback removed): an unconfigured/disabled
		// archive is simply unavailable, not a Gmail-backed surface.
		return NativeStatus{Source: "unavailable", Available: false}, nil
	}
	status := NativeStatus{
		Source:         "archive",
		Available:      true,
		OfflineCapable: true,
		GeneratedAt:    r.now(),
		Overlay:        nativeOverlayStatus(r.state.Snapshot()),
	}
	c, err := dialIMAP(ctx, r.cfg.Addr, r.cfg.Timeout)
	if err != nil {
		status.Available = false
		return status, err
	}
	defer c.close()
	if err := c.login(r.cfg.User, r.cfg.Pass); err != nil {
		status.Available = false
		return status, err
	}
	defer c.logout()

	snapshot := r.state.Snapshot()
	for _, mailbox := range r.cfg.Mailboxes {
		mailbox = strings.TrimSpace(mailbox)
		if mailbox == "" {
			continue
		}
		if err := c.examine(mailbox); err != nil {
			status.Mailboxes = append(status.Mailboxes, NativeMailboxStatus{
				Name:              mailbox,
				AttachmentCapable: true,
			})
			continue
		}
		uids, err := c.uidSearch("ALL")
		if err != nil {
			status.Mailboxes = append(status.Mailboxes, NativeMailboxStatus{
				Name:              mailbox,
				AttachmentCapable: true,
			})
			continue
		}
		mb := NativeMailboxStatus{
			Name:              mailbox,
			Total:             len(uids),
			LatestUID:         latestUID(uids),
			AttachmentCapable: true,
		}
		if strings.EqualFold(mailbox, "INBOX") {
			mb.Unread = len(uids)
		}
		for _, st := range snapshot {
			if st.Mailbox != mailbox || st.UID == "" {
				continue
			}
			if st.Read {
				mb.LocallyRead++
			}
			if st.Archived {
				mb.LocallyArchived++
			}
			if st.Trashed {
				mb.LocallyTrashed++
			}
			if strings.EqualFold(mailbox, "INBOX") && (st.Read || st.Archived || st.Trashed) {
				mb.Unread--
			}
		}
		if mb.Unread < 0 {
			mb.Unread = 0
		}
		status.Mailboxes = append(status.Mailboxes, mb)
	}
	return status, nil
}

func (r *Repository) archiveEnabled() bool {
	return r != nil &&
		strings.TrimSpace(r.cfg.Addr) != "" &&
		strings.TrimSpace(r.cfg.User) != "" &&
		strings.TrimSpace(r.cfg.Pass) != ""
}

func (r *Repository) fallbackSearchPage(ctx context.Context, query, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error) {
	if r != nil && r.fallback != nil {
		return r.fallback.SearchPage(ctx, query, pageToken, maxResults)
	}
	return nil, "", ErrArchiveUnavailable
}

func (r *Repository) getArchiveParsed(ctx context.Context, messageID string) (*lmtpd.Message, error) {
	if messageID == "" {
		return nil, ErrArchiveNotFound
	}
	if mailbox, uid, ok := archiveLocatorParts(messageID); ok {
		return r.fetchArchiveUID(ctx, mailbox, uid)
	}
	if st := r.state.Get(messageID); st.Mailbox != "" && st.UID != "" {
		return r.fetchArchiveUID(ctx, st.Mailbox, st.UID)
	}
	return r.searchArchiveByMessageID(ctx, messageID)
}

func (r *Repository) fetchArchiveUID(ctx context.Context, mailbox, uid string) (*lmtpd.Message, error) {
	c, err := dialIMAP(ctx, r.cfg.Addr, r.cfg.Timeout)
	if err != nil {
		return nil, err
	}
	defer c.close()
	if err := c.login(r.cfg.User, r.cfg.Pass); err != nil {
		return nil, err
	}
	defer c.logout()
	for _, candidate := range lookupMailboxCandidates(mailbox) {
		parsed, err := r.fetchArchiveUIDAfterLogin(c, candidate, uid)
		if err == nil {
			return parsed, nil
		}
	}
	return nil, ErrArchiveNotFound
}

func (r *Repository) fetchArchiveUIDAfterLogin(c *imapConn, mailbox, uid string) (*lmtpd.Message, error) {
	if err := c.examine(mailbox); err != nil {
		return nil, err
	}
	msgs, err := c.uidFetchMessages(uid)
	if err != nil {
		return nil, err
	}
	for _, msg := range msgs {
		if msg.UID != "" && msg.UID != uid {
			continue
		}
		parsed, err := lmtpd.ParseMessage(msg.Raw, archiveLocator(mailbox, uid))
		if err != nil {
			return nil, err
		}
		if parsed != nil && parsed.Detail != nil {
			_ = r.state.RememberLocator(parsed.Detail.ID, mailbox, uid)
			return parsed, nil
		}
	}
	return nil, ErrArchiveNotFound
}

func (r *Repository) searchArchiveByMessageID(ctx context.Context, messageID string) (*lmtpd.Message, error) {
	c, err := dialIMAP(ctx, r.cfg.Addr, r.cfg.Timeout)
	if err != nil {
		return nil, err
	}
	defer c.close()
	if err := c.login(r.cfg.User, r.cfg.Pass); err != nil {
		return nil, err
	}
	defer c.logout()

	candidates := []string{messageID}
	if !strings.HasPrefix(messageID, "<") {
		candidates = append(candidates, "<"+messageID+">")
	}
	for _, mailbox := range r.cfg.Mailboxes {
		mailbox = strings.TrimSpace(mailbox)
		if mailbox == "" {
			continue
		}
		if err := c.examine(mailbox); err != nil {
			continue
		}
		for _, candidate := range candidates {
			uids, err := c.uidSearch("HEADER \"Message-ID\" " + quote(candidate))
			if err != nil || len(uids) == 0 {
				continue
			}
			return r.fetchArchiveUID(ctx, mailbox, uids[len(uids)-1])
		}
	}
	return nil, ErrArchiveNotFound
}

func (r *Repository) applyArchiveLabelMutation(messageID string, removeNames []string) error {
	var archive bool
	var read bool
	for _, name := range removeNames {
		switch strings.ToUpper(strings.TrimSpace(name)) {
		case "INBOX":
			archive = true
			read = true
		case "UNREAD":
			read = true
		}
	}
	if archive {
		return r.state.MarkArchived(messageID)
	}
	if read {
		return r.state.MarkRead(messageID)
	}
	return nil
}

func (r *Repository) canMutateArchiveMessage(ctx context.Context, messageID string) bool {
	if r == nil || !r.archiveEnabled() {
		return false
	}
	if r.state.Known(messageID) {
		return true
	}
	_, err := r.getArchiveParsed(ctx, messageID)
	return err == nil
}
