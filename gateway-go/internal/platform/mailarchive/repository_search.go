package mailarchive

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive/overlay"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailbody"
)

const defaultArchivePageSize = 25

const (
	evidenceMailPreviewBytes    = 128 << 10
	evidenceMailFetchMultiplier = 3
)

type archiveSearchPlan struct {
	spec         archiveQuery
	mailboxes    []string
	offset       int
	pageSize     int
	fetchPerBox  int
	previewBytes int
}

type archiveRow struct {
	summary gmail.MessageSummary
	when    time.Time
	uid     int
}

func planArchiveSearch(configured []string, spec archiveQuery, pageToken string, maxResults int) (archiveSearchPlan, error) {
	if maxResults <= 0 {
		maxResults = defaultArchivePageSize
	}
	offset, err := parseArchivePageToken(pageToken)
	if err != nil {
		return archiveSearchPlan{}, err
	}
	mailboxes := cleanMailboxes(archiveSearchMailboxes(configured, spec))
	if len(mailboxes) == 0 {
		return archiveSearchPlan{}, ErrArchiveUnavailable
	}
	return archiveSearchPlan{
		spec:        spec,
		mailboxes:   mailboxes,
		offset:      offset,
		pageSize:    maxResults,
		fetchPerBox: archiveFetchLimit(offset, maxResults),
	}, nil
}

func archiveFetchLimit(offset, pageSize int) int {
	// Return the cap before doing arithmetic that could overflow on an untrusted
	// page size or token. Any input this large already requires the maximum scan.
	if offset >= maxArchiveFetchPerBox || pageSize >= maxArchiveFetchPerBox {
		return maxArchiveFetchPerBox
	}
	wanted := offset + pageSize*archivePostFilterScanMultiplier + 1
	if wanted < minArchiveFetchPerBox {
		return minArchiveFetchPerBox
	}
	if wanted > maxArchiveFetchPerBox {
		return maxArchiveFetchPerBox
	}
	return wanted
}

func (r *Repository) searchArchive(ctx context.Context, spec archiveQuery, pageToken string, maxResults int, boundedEvidence bool) ([]gmail.MessageSummary, string, error) {
	plan, err := planArchiveSearch(r.cfg.Mailboxes, spec, pageToken, maxResults)
	if err != nil {
		return nil, "", err
	}
	if boundedEvidence {
		boundedFetch := maxArchiveFetchPerBox
		if plan.pageSize < maxArchiveFetchPerBox/evidenceMailFetchMultiplier {
			boundedFetch = max(plan.pageSize*evidenceMailFetchMultiplier+1, plan.pageSize+1)
		}
		plan.fetchPerBox = min(plan.fetchPerBox, boundedFetch)
		plan.previewBytes = evidenceMailPreviewBytes
	}
	c, err := r.openArchiveSearch(ctx)
	if err != nil {
		return nil, "", err
	}
	stopCancelClose := c.closeOnContext(ctx)
	defer stopCancelClose()
	defer c.close()
	defer c.logout()

	stateSnapshot := r.state.Snapshot()
	locators := make(map[string]overlay.MessageState)
	rows, scanErr := r.scanArchiveMailboxes(ctx, c, plan, stateSnapshot, locators)
	if rememberErr := r.state.RememberLocators(locators); rememberErr != nil && scanErr == nil {
		scanErr = fmt.Errorf("%w: persist locators: %w", ErrArchivePartial, rememberErr)
	}
	if scanErr != nil && !errors.Is(scanErr, ErrArchivePartial) {
		return nil, "", scanErr
	}
	page, next := pageArchiveRows(rows, plan)
	return page, next, scanErr
}

func (r *Repository) openArchiveSearch(ctx context.Context) (*imapConn, error) {
	c, err := dialIMAP(ctx, r.cfg.Addr, r.cfg.Timeout)
	if err != nil {
		return nil, err
	}
	if err := c.login(r.cfg.User, r.cfg.Pass); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (r *Repository) scanArchiveMailboxes(
	ctx context.Context,
	c *imapConn,
	plan archiveSearchPlan,
	stateSnapshot map[string]overlay.MessageState,
	locators map[string]overlay.MessageState,
) ([]archiveRow, error) {
	var rows []archiveRow
	var scanErrs []error
	completed := 0
	seen := make(map[string]bool)
	for _, mailbox := range plan.mailboxes {
		if err := ctx.Err(); err != nil {
			scanErrs = append(scanErrs, err)
			break
		}
		mailboxRows, err := r.scanArchiveMailbox(ctx, c, mailbox, plan, seen, stateSnapshot, locators)
		if err != nil {
			scanErrs = append(scanErrs, err)
			if !errors.Is(err, ErrArchivePartial) {
				continue
			}
		}
		completed++
		rows = append(rows, mailboxRows...)
	}
	if completed > 0 {
		if len(scanErrs) > 0 {
			return rows, fmt.Errorf("%w: %w", ErrArchivePartial, errors.Join(scanErrs...))
		}
		return rows, nil
	}
	return nil, fmt.Errorf("%w: no mailbox scan completed: %w", ErrArchiveUnavailable, errors.Join(scanErrs...))
}

func (r *Repository) scanArchiveMailbox(
	ctx context.Context,
	c *imapConn,
	mailbox string,
	plan archiveSearchPlan,
	seen map[string]bool,
	stateSnapshot map[string]overlay.MessageState,
	locators map[string]overlay.MessageState,
) ([]archiveRow, error) {
	if err := c.examine(mailbox); err != nil {
		return nil, fmt.Errorf("mailarchive: examine %q: %w", mailbox, err)
	}
	uids, err := c.uidSearchSentAware(plan.spec.Criteria)
	if err != nil {
		return nil, fmt.Errorf("mailarchive: search %q: %w", mailbox, err)
	}
	uids = tailStrings(uids, plan.fetchPerBox)
	reverseStrings(uids)
	var messages []FetchedMessage
	if plan.previewBytes > 0 {
		messages, err = c.uidFetchMessagePreviews(strings.Join(uids, ","), plan.previewBytes)
	} else {
		messages, err = c.uidFetchMessages(strings.Join(uids, ","))
	}
	if err != nil {
		return nil, fmt.Errorf("mailarchive: fetch %q: %w", mailbox, err)
	}
	rows, filterErr := r.filterArchiveMessages(ctx, mailbox, messages, plan.spec, seen, stateSnapshot, locators)
	if filterErr != nil {
		return rows, filterErr
	}
	for _, message := range messages {
		if message.Truncated {
			return rows, fmt.Errorf("%w: message preview truncated", ErrArchivePartial)
		}
	}
	return rows, nil
}

func (r *Repository) filterArchiveMessages(
	ctx context.Context,
	mailbox string,
	messages []FetchedMessage,
	spec archiveQuery,
	seen map[string]bool,
	stateSnapshot map[string]overlay.MessageState,
	locators map[string]overlay.MessageState,
) ([]archiveRow, error) {
	rows := make([]archiveRow, 0, len(messages))
	for _, message := range messages {
		if err := ctx.Err(); err != nil {
			return rows, err
		}
		if row, ok := r.archiveRowForMessage(mailbox, message, spec, seen, stateSnapshot, locators); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (r *Repository) archiveRowForMessage(
	mailbox string,
	message FetchedMessage,
	spec archiveQuery,
	seen map[string]bool,
	stateSnapshot map[string]overlay.MessageState,
	locators map[string]overlay.MessageState,
) (archiveRow, bool) {
	uid := strings.TrimSpace(message.UID)
	if uid == "" {
		return archiveRow{}, false
	}
	parsed, err := lmtpd.ParseMessage(message.Raw, archiveLocator(mailbox, uid))
	if err != nil || parsed == nil {
		return archiveRow{}, false
	}
	if parsed.Detail == nil {
		return archiveRow{}, false
	}
	detail := parsed.Detail
	id := archiveMessageID(detail.ID, mailbox, uid)
	if seen[id] {
		return archiveRow{}, false
	}
	seen[id] = true
	locators[id] = overlay.MessageState{Mailbox: mailbox, UID: uid}
	state := stateSnapshot[id]
	if !archiveMessageVisible(spec, detail, state) {
		return archiveRow{}, false
	}
	return archiveRow{
		summary: detailToSummary(detail, mailbox, state, spec.Text),
		when:    mailbody.ParseMailDate(detail.Date),
		uid:     parseUID(uid),
	}, true
}

func archiveMessageID(messageID, mailbox, uid string) string {
	if id := strings.TrimSpace(messageID); id != "" {
		return id
	}
	return archiveLocator(mailbox, uid)
}

func archiveMessageVisible(spec archiveQuery, detail *gmail.MessageDetail, state overlay.MessageState) bool {
	if state.Trashed {
		return false
	}
	if state.Archived && (spec.InboxOnly || spec.DefaultView) {
		return false
	}
	if spec.HasAttachment && len(detail.Attachments) == 0 {
		return false
	}
	// Day-pager post-filter: keep rows whose Date header falls in
	// [SentSince, SentBefore). Required when IMAP SENTSINCE was rejected and
	// the search fell back to ALL/SINCE (uidSearchSentAware).
	if !spec.SentSince.IsZero() || !spec.SentBefore.IsZero() {
		return sentInHalfOpenRange(detail.Date, spec.SentSince, spec.SentBefore)
	}
	return true
}

func pageArchiveRows(rows []archiveRow, plan archiveSearchPlan) ([]gmail.MessageSummary, string) {
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].when.Equal(rows[j].when) {
			return rows[i].when.After(rows[j].when)
		}
		return rows[i].uid > rows[j].uid
	})
	if plan.offset >= len(rows) {
		return nil, ""
	}
	count := plan.pageSize
	if remaining := len(rows) - plan.offset; count > remaining {
		count = remaining
	}
	end := plan.offset + count
	page := make([]gmail.MessageSummary, 0, count)
	for _, row := range rows[plan.offset:end] {
		page = append(page, row.summary)
	}
	next := ""
	if end < len(rows) {
		next = archivePageTokenPrefix + strconv.Itoa(end)
	}
	return page, next
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
