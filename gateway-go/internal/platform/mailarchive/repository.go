package mailarchive

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
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

type RepositoryOptions struct {
	StatePath string
	Fallback  FallbackClient
	Now       func() time.Time
}

type NativeStatus struct {
	Source         string
	Available      bool
	OfflineCapable bool
	Mailboxes      []NativeMailboxStatus
	Overlay        NativeOverlayStatus
	GeneratedAt    time.Time
}

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

type NativeOverlayStatus struct {
	Messages int
	Read     int
	Archived int
	Trashed  int
}

// Repository exposes the on-box IMAP archive through the Gmail-like interface
// already used by miniapp.gmail.*. Reads prefer the local archive; Gmail remains
// a compatibility fallback for disabled archive setups and unsupported legacy
// Gmail search tokens.
type Repository struct {
	cfg      Config
	state    *StateStore
	fallback FallbackClient
	now      func() time.Time
}

func NewRepository(cfg Config, opts RepositoryOptions) *Repository {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if len(cfg.Mailboxes) == 0 {
		cfg.Mailboxes = defaultMailboxes()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Repository{
		cfg:      cfg,
		state:    NewStateStore(opts.StatePath),
		fallback: opts.Fallback,
		now:      now,
	}
}

func defaultMailboxes() []string {
	return []string{"INBOX", "Gmail"}
}

func (r *Repository) Search(ctx context.Context, query string, maxResults int) ([]gmail.MessageSummary, error) {
	rows, _, err := r.SearchPage(ctx, query, "", maxResults)
	return rows, err
}

func (r *Repository) SearchPage(ctx context.Context, query, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error) {
	if r == nil || !r.archiveEnabled() {
		return r.fallbackSearchPage(ctx, query, pageToken, maxResults)
	}
	if strings.TrimSpace(pageToken) != "" && !strings.HasPrefix(pageToken, archivePageTokenPrefix) {
		return r.fallbackSearchPage(ctx, query, pageToken, maxResults)
	}
	spec, err := parseArchiveQuery(query, r.now())
	if err != nil {
		if r.fallback != nil {
			return r.fallback.SearchPage(ctx, query, pageToken, maxResults)
		}
		return nil, "", err
	}
	rows, next, err := r.searchArchive(ctx, spec, pageToken, maxResults)
	if err != nil {
		if r.fallback != nil {
			return r.fallback.SearchPage(ctx, query, pageToken, maxResults)
		}
		return nil, "", err
	}
	return rows, next, nil
}

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

func (r *Repository) NativeStatus(ctx context.Context) (NativeStatus, error) {
	if r == nil || !r.archiveEnabled() {
		return NativeStatus{Source: "gmail", Available: r != nil && r.fallback != nil}, nil
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

type archiveQuery struct {
	Criteria      string
	DefaultView   bool
	HasAttachment bool
	InboxOnly     bool
}

func parseArchiveQuery(query string, now time.Time) (archiveQuery, error) {
	q := strings.TrimSpace(query)
	defaultView := isDefaultArchiveViewQuery(q)
	inboxOnly := inInboxRe.MatchString(q) && !defaultView
	if inAnywhereRe.MatchString(q) {
		inboxOnly = false
	}
	since := time.Time{}
	if defaultView {
		since = now.Add(-defaultNativeLookback)
	}
	if m := newerThanRe.FindStringSubmatch(q); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n <= 0 {
			return archiveQuery{}, ErrArchiveUnsupportedQuery
		}
		switch strings.ToLower(m[2]) {
		case "d":
			since = now.Add(-time.Duration(n) * 24 * time.Hour)
		case "m":
			since = now.AddDate(0, -n, 0)
		case "y":
			since = now.AddDate(-n, 0, 0)
		default:
			return archiveQuery{}, ErrArchiveUnsupportedQuery
		}
	}

	hasAttachment := hasAttachmentRe.MatchString(q)
	from := extractFromQuery(q)
	text := normalizeArchiveTextQuery(q)
	hasUnsupportedOnly := unsupportedOperatorRe.MatchString(text)
	text = unsupportedOperatorRe.ReplaceAllString(text, " ")
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))
	if hasUnsupportedOnly && from == "" && text == "" && !defaultView {
		return archiveQuery{}, ErrArchiveUnsupportedQuery
	}

	var parts []string
	if !since.IsZero() {
		parts = append(parts, "SINCE "+imapSinceDate(since))
	}
	switch {
	case from != "" && text != "":
		parts = append(parts, "FROM "+quote(from), "TEXT "+quote(text))
	case from != "":
		parts = append(parts, "FROM "+quote(from))
	case text != "":
		parts = append(parts, fmt.Sprintf("OR OR FROM %s SUBJECT %s TEXT %s", quote(text), quote(text), quote(text)))
	}
	if len(parts) == 0 {
		parts = append(parts, "ALL")
	}
	return archiveQuery{Criteria: strings.Join(parts, " "), DefaultView: defaultView, HasAttachment: hasAttachment, InboxOnly: inboxOnly}, nil
}

func isDefaultArchiveViewQuery(q string) bool {
	q = strings.TrimSpace(q)
	return q == "" ||
		strings.EqualFold(q, "{in:inbox is:unread} newer_than:30d") ||
		// Older native builds sent the previous default explicitly. Treat it as
		// the native recent view while still honoring its 7-day lookback.
		strings.EqualFold(q, "{in:inbox is:unread} newer_than:7d")
}

var (
	newerThanRe           = regexp.MustCompile(`(?i)\bnewer_than:(\d+)([dmy])\b`)
	fromQueryRe           = regexp.MustCompile(`(?i)\bfrom:(?:"([^"]+)"|([^\s}]+))`)
	hasAttachmentRe       = regexp.MustCompile(`(?i)\bhas:attachment\b`)
	inInboxRe             = regexp.MustCompile(`(?i)\bin:inbox\b`)
	inAnywhereRe          = regexp.MustCompile(`(?i)\bin:anywhere\b`)
	stripQuerySyntaxRe    = regexp.MustCompile(`(?i)[{}]|\bin:(?:inbox|anywhere)\b|\bis:unread\b|\bnewer_than:\d+[dmy]\b|\bhas:attachment\b|\bfrom:(?:"[^"]+"|[^\s}]+)`)
	unsupportedOperatorRe = regexp.MustCompile(`(?i)\b(?:is|in|label|has|category|after|before|older_than):[^\s}]+`)
)

func extractFromQuery(query string) string {
	m := fromQueryRe.FindStringSubmatch(query)
	if m == nil {
		return ""
	}
	if strings.TrimSpace(m[1]) != "" {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(m[2])
}

func normalizeArchiveTextQuery(query string) string {
	text := stripQuerySyntaxRe.ReplaceAllString(query, " ")
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}

func archiveSearchMailboxes(configured []string, spec archiveQuery) []string {
	if !spec.InboxOnly {
		return configured
	}
	var out []string
	for _, mailbox := range configured {
		if strings.EqualFold(strings.TrimSpace(mailbox), "INBOX") {
			out = append(out, mailbox)
		}
	}
	return out
}

type archiveRow struct {
	summary gmail.MessageSummary
	when    time.Time
	uid     int
}

func (r *Repository) searchArchive(ctx context.Context, spec archiveQuery, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error) {
	if maxResults <= 0 {
		maxResults = 25
	}
	offset, err := parseArchivePageToken(pageToken)
	if err != nil {
		return nil, "", err
	}
	fetchPerBox := offset + maxResults*archivePostFilterScanMultiplier + 1
	if fetchPerBox < minArchiveFetchPerBox {
		fetchPerBox = minArchiveFetchPerBox
	}
	if fetchPerBox > maxArchiveFetchPerBox {
		fetchPerBox = maxArchiveFetchPerBox
	}

	c, err := dialIMAP(ctx, r.cfg.Addr, r.cfg.Timeout)
	if err != nil {
		return nil, "", err
	}
	defer c.close()
	if err := c.login(r.cfg.User, r.cfg.Pass); err != nil {
		return nil, "", err
	}
	defer c.logout()

	var all []archiveRow
	seen := map[string]bool{}
	for _, mailbox := range archiveSearchMailboxes(r.cfg.Mailboxes, spec) {
		mailbox = strings.TrimSpace(mailbox)
		if mailbox == "" {
			continue
		}
		if err := c.examine(mailbox); err != nil {
			continue
		}
		uids, err := c.uidSearch(spec.Criteria)
		if err != nil {
			continue
		}
		uids = tailStrings(uids, fetchPerBox)
		reverseStrings(uids)
		msgs, err := c.uidFetchMessages(strings.Join(uids, ","))
		if err != nil {
			continue
		}
		for _, msg := range msgs {
			uid := strings.TrimSpace(msg.UID)
			if uid == "" {
				continue
			}
			parsed, err := lmtpd.ParseMessage(msg.Raw, archiveLocator(mailbox, uid))
			if err != nil || parsed == nil || parsed.Detail == nil {
				continue
			}
			detail := parsed.Detail
			id := strings.TrimSpace(detail.ID)
			if id == "" {
				id = archiveLocator(mailbox, uid)
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			_ = r.state.RememberLocator(id, mailbox, uid)
			st := r.state.Get(id)
			if st.Trashed || (spec.InboxOnly && st.Archived) || (spec.DefaultView && (st.Archived || st.Read)) {
				continue
			}
			if spec.HasAttachment && len(detail.Attachments) == 0 {
				continue
			}
			row := detailToSummary(detail, mailbox, st)
			all = append(all, archiveRow{
				summary: row,
				when:    parseMailDate(detail.Date),
				uid:     parseUID(uid),
			})
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].when.Equal(all[j].when) {
			return all[i].when.After(all[j].when)
		}
		return all[i].uid > all[j].uid
	})
	if offset >= len(all) {
		return nil, "", nil
	}
	end := offset + maxResults
	if end > len(all) {
		end = len(all)
	}
	rows := make([]gmail.MessageSummary, 0, end-offset)
	for _, row := range all[offset:end] {
		rows = append(rows, row.summary)
	}
	next := ""
	if end < len(all) {
		next = archivePageTokenPrefix + strconv.Itoa(end)
	}
	return rows, next, nil
}

func parseArchivePageToken(token string) (int, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, nil
	}
	if !strings.HasPrefix(token, archivePageTokenPrefix) {
		return 0, ErrArchiveUnsupportedQuery
	}
	n, err := strconv.Atoi(strings.TrimPrefix(token, archivePageTokenPrefix))
	if err != nil || n < 0 {
		return 0, ErrArchiveUnsupportedQuery
	}
	return n, nil
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

func detailToSummary(detail *gmail.MessageDetail, mailbox string, st MessageState) gmail.MessageSummary {
	return gmail.MessageSummary{
		ID:              detail.ID,
		ThreadID:        detail.ThreadID,
		From:            detail.From,
		Subject:         detail.Subject,
		Date:            detail.Date,
		Snippet:         snippetFromBody(detail.Body),
		Labels:          labelsForArchiveMessage(mailbox, st),
		Mailbox:         mailbox,
		HasAttachment:   len(detail.Attachments) > 0,
		AttachmentCount: len(detail.Attachments),
	}
}

func (r *Repository) applyStateToDetail(detail *gmail.MessageDetail, st MessageState) {
	if detail == nil {
		return
	}
	mailbox := st.Mailbox
	if mailbox == "" {
		mailbox = "INBOX"
	}
	detail.Labels = labelsForArchiveMessage(mailbox, st)
}

func labelsForArchiveMessage(mailbox string, st MessageState) []string {
	if st.Trashed {
		return []string{"TRASH"}
	}
	var labels []string
	if strings.EqualFold(strings.TrimSpace(mailbox), "INBOX") && !st.Archived {
		labels = append(labels, "INBOX")
		if !st.Read {
			labels = append(labels, "UNREAD")
		}
	}
	if labels == nil {
		return []string{}
	}
	return labels
}

func cloneDetail(detail *gmail.MessageDetail) *gmail.MessageDetail {
	if detail == nil {
		return nil
	}
	cp := *detail
	cp.Labels = append([]string(nil), detail.Labels...)
	cp.Attachments = append([]gmail.AttachmentInfo(nil), detail.Attachments...)
	cp.References = append([]string(nil), detail.References...)
	return &cp
}

func snippetFromBody(body string) string {
	body = strings.TrimSpace(strings.Join(strings.Fields(body), " "))
	const max = 360
	if len([]rune(body)) <= max {
		return body
	}
	runes := []rune(body)
	return string(runes[:max]) + "..."
}

func parseMailDate(raw string) time.Time {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}
	}
	if t, err := mail.ParseDate(raw); err == nil {
		return t
	}
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		"Mon, 02 Jan 2006 15:04:05 -0700",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func archiveLocator(mailbox, uid string) string {
	return archiveLocatorPrefix + url.QueryEscape(mailbox) + "|" + url.QueryEscape(uid)
}

func archiveLocatorParts(id string) (string, string, bool) {
	if !strings.HasPrefix(id, archiveLocatorPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(id, archiveLocatorPrefix)
	parts := strings.Split(rest, "|")
	if len(parts) != 2 {
		return "", "", false
	}
	mailbox, err1 := url.QueryUnescape(parts[0])
	uid, err2 := url.QueryUnescape(parts[1])
	if err1 != nil || err2 != nil || mailbox == "" || uid == "" {
		return "", "", false
	}
	return mailbox, uid, true
}

func tailStrings(in []string, n int) []string {
	if n <= 0 || len(in) <= n {
		return append([]string(nil), in...)
	}
	return append([]string(nil), in[len(in)-n:]...)
}

func reverseStrings(in []string) {
	for i, j := 0, len(in)-1; i < j; i, j = i+1, j-1 {
		in[i], in[j] = in[j], in[i]
	}
}

func parseUID(uid string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(uid))
	return n
}

func latestUID(uids []string) string {
	if len(uids) == 0 {
		return ""
	}
	latest := strings.TrimSpace(uids[0])
	latestN := parseUID(latest)
	for _, uid := range uids[1:] {
		n := parseUID(uid)
		if n > latestN {
			latest = strings.TrimSpace(uid)
			latestN = n
		}
	}
	return latest
}

func nativeOverlayStatus(snapshot map[string]MessageState) NativeOverlayStatus {
	var out NativeOverlayStatus
	out.Messages = len(snapshot)
	for _, st := range snapshot {
		if st.Read {
			out.Read++
		}
		if st.Archived {
			out.Archived++
		}
		if st.Trashed {
			out.Trashed++
		}
	}
	return out
}
