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

type archiveSearchPlan struct {
	spec        archiveQuery
	mailboxes   []string
	offset      int
	pageSize    int
	fetchPerBox int
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

func (r *Repository) searchArchive(ctx context.Context, spec archiveQuery, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error) {
	plan, err := planArchiveSearch(r.cfg.Mailboxes, spec, pageToken, maxResults)
	if err != nil {
		return nil, "", err
	}
	c, err := r.openArchiveSearch(ctx)
	if err != nil {
		return nil, "", err
	}
	defer c.close()
	defer c.logout()

	rows, err := r.scanArchiveMailboxes(c, plan)
	if err != nil {
		return nil, "", err
	}
	page, next := pageArchiveRows(rows, plan)
	return page, next, nil
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

func (r *Repository) scanArchiveMailboxes(c *imapConn, plan archiveSearchPlan) ([]archiveRow, error) {
	var rows []archiveRow
	var scanErrs []error
	completed := 0
	seen := make(map[string]bool)
	for _, mailbox := range plan.mailboxes {
		mailboxRows, err := r.scanArchiveMailbox(c, mailbox, plan, seen)
		if err != nil {
			scanErrs = append(scanErrs, err)
			continue
		}
		completed++
		rows = append(rows, mailboxRows...)
	}
	if completed > 0 {
		return rows, nil
	}
	return nil, fmt.Errorf("%w: no mailbox scan completed: %w", ErrArchiveUnavailable, errors.Join(scanErrs...))
}

func (r *Repository) scanArchiveMailbox(c *imapConn, mailbox string, plan archiveSearchPlan, seen map[string]bool) ([]archiveRow, error) {
	if err := c.examine(mailbox); err != nil {
		return nil, fmt.Errorf("mailarchive: examine %q: %w", mailbox, err)
	}
	uids, err := c.uidSearch(plan.spec.Criteria)
	if err != nil {
		return nil, fmt.Errorf("mailarchive: search %q: %w", mailbox, err)
	}
	uids = tailStrings(uids, plan.fetchPerBox)
	reverseStrings(uids)
	messages, err := c.uidFetchMessages(strings.Join(uids, ","))
	if err != nil {
		return nil, fmt.Errorf("mailarchive: fetch %q: %w", mailbox, err)
	}
	return r.filterArchiveMessages(mailbox, messages, plan.spec, seen), nil
}

func (r *Repository) filterArchiveMessages(mailbox string, messages []FetchedMessage, spec archiveQuery, seen map[string]bool) []archiveRow {
	rows := make([]archiveRow, 0, len(messages))
	for _, message := range messages {
		if row, ok := r.archiveRowForMessage(mailbox, message, spec, seen); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func (r *Repository) archiveRowForMessage(mailbox string, message FetchedMessage, spec archiveQuery, seen map[string]bool) (archiveRow, bool) {
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
	_ = r.state.RememberLocator(id, mailbox, uid)
	state := r.state.Get(id)
	if !archiveMessageVisible(spec, detail, state) {
		return archiveRow{}, false
	}
	return archiveRow{
		summary: detailToSummary(detail, mailbox, state),
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
	return !spec.HasAttachment || len(detail.Attachments) > 0
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
