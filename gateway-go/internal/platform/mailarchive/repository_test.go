package mailarchive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestRepositorySearchPageUsesArchiveAndLocalOverlay(t *testing.T) {
	raw := archiveTestMessage("m1@example.com", "sender@example.com", "Archive subject", "Archive body for native mail.", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": {"1": []byte(raw)},
	})
	repo := NewRepository(Config{
		Addr:      srv.addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX"},
		Timeout:   time.Second,
	}, RepositoryOptions{
		StatePath: t.TempDir() + "/state.json",
		Now:       func() time.Time { return time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC) },
	})

	rows, next, err := repo.SearchPage(context.Background(), "{in:inbox is:unread} newer_than:7d", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if next != "" {
		t.Fatalf("next = %q, want empty", next)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1: %#v", len(rows), rows)
	}
	row := rows[0]
	if row.ID != "m1@example.com" {
		t.Fatalf("row.ID = %q, want stable Message-ID", row.ID)
	}
	if row.Subject != "Archive subject" || !strings.Contains(row.Snippet, "Archive body") {
		t.Fatalf("unexpected row = %#v", row)
	}
	if !hasLabel(row.Labels, "INBOX") || !hasLabel(row.Labels, "UNREAD") {
		t.Fatalf("labels = %v, want INBOX+UNREAD", row.Labels)
	}

	if err := repo.ModifyLabels(context.Background(), row.ID, nil, []string{"UNREAD"}); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	rows, _, err = repo.SearchPage(context.Background(), "{in:inbox is:unread} newer_than:7d", "", 10)
	if err != nil {
		t.Fatalf("SearchPage after read: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != row.ID || hasLabel(rows[0].Labels, "UNREAD") {
		t.Fatalf("default rows after read = %#v, want same row without UNREAD", rows)
	}
	rows, _, err = repo.SearchPage(context.Background(), `from:"sender@example.com"`, "", 10)
	if err != nil {
		t.Fatalf("custom SearchPage after read: %v", err)
	}
	if len(rows) != 1 || hasLabel(rows[0].Labels, "UNREAD") {
		t.Fatalf("custom labels after read = %#v, want no UNREAD", rows)
	}

	if err := repo.ModifyLabels(context.Background(), row.ID, nil, []string{"INBOX"}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	rows, _, err = repo.SearchPage(context.Background(), "{in:inbox is:unread} newer_than:7d", "", 10)
	if err != nil {
		t.Fatalf("SearchPage after archive: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("default rows after archive = %#v, want hidden", rows)
	}
	rows, _, err = repo.SearchPage(context.Background(), `from:"sender@example.com"`, "", 10)
	if err != nil {
		t.Fatalf("custom SearchPage after archive: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("custom rows after archive = %#v, want archived row discoverable", rows)
	}
}

func TestRepositoryGetAttachmentUsesArchiveRawMessage(t *testing.T) {
	raw := archiveTestMessage("m2@example.com", "sender@example.com", "Attachment", "See attached.", "%PDF")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": {"7": []byte(raw)},
	})
	repo := NewRepository(Config{
		Addr:      srv.addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX"},
		Timeout:   time.Second,
	}, RepositoryOptions{StatePath: t.TempDir() + "/state.json"})

	rows, _, err := repo.SearchPage(context.Background(), "", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	data, err := repo.GetAttachment(context.Background(), rows[0].ID, "att-0")
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if string(data) != "%PDF" {
		t.Fatalf("attachment = %q, want %%PDF", data)
	}
}

func TestRepositoryResolvesLegacyGmailLocatorAfterArchiveRename(t *testing.T) {
	raw := archiveTestMessage("m-legacy@example.com", "sender@example.com", "Renamed archive", "Still reachable.", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"Archive": {"9": []byte(raw)},
	})
	repo := NewRepository(Config{
		Addr:      srv.addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX", "Archive"},
		Timeout:   time.Second,
	}, RepositoryOptions{StatePath: t.TempDir() + "/state.json"})

	msg, err := repo.GetMessage(context.Background(), archiveLocator("Gmail", "9"))
	if err != nil {
		t.Fatalf("GetMessage legacy Gmail locator: %v", err)
	}
	if msg.ID != "m-legacy@example.com" || msg.Subject != "Renamed archive" {
		t.Fatalf("message = %#v, want renamed archive mail", msg)
	}
}

func TestRepositoryNativeStatusSummarizesArchiveAndOverlay(t *testing.T) {
	raw1 := archiveTestMessage("m1@example.com", "sender@example.com", "One", "Body one.", "")
	raw2 := archiveTestMessage("m2@example.com", "sender@example.com", "Two", "Body two.", "%PDF")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": {"1": []byte(raw1), "2": []byte(raw2)},
	})
	repo := NewRepository(Config{
		Addr:      srv.addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX"},
		Timeout:   time.Second,
	}, RepositoryOptions{StatePath: t.TempDir() + "/state.json"})

	rows, _, err := repo.SearchPage(context.Background(), "", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if err := repo.ModifyLabels(context.Background(), rows[0].ID, nil, []string{"UNREAD"}); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	status, err := repo.NativeStatus(context.Background())
	if err != nil {
		t.Fatalf("NativeStatus: %v", err)
	}
	if status.Source != "archive" || !status.Available || !status.OfflineCapable {
		t.Fatalf("status source/availability = %#v", status)
	}
	if len(status.Mailboxes) != 1 {
		t.Fatalf("mailboxes = %#v, want one", status.Mailboxes)
	}
	mb := status.Mailboxes[0]
	if mb.Name != "INBOX" || mb.Total != 2 || mb.Unread != 1 || mb.LocallyRead != 1 || mb.LatestUID != "2" || !mb.AttachmentCapable {
		t.Fatalf("mailbox status = %#v", mb)
	}
	if status.Overlay.Messages != 2 || status.Overlay.Read != 1 {
		t.Fatalf("overlay = %#v, want locator snapshots plus one read", status.Overlay)
	}
}

func TestRepositorySearchPageFiltersHasAttachmentInArchive(t *testing.T) {
	rawPlain := archiveTestMessage("plain@example.com", "sender@example.com", "Plain", "No attachment.", "")
	rawAttached := archiveTestMessage("attached@example.com", "sender@example.com", "Attached", "See attached.", "%PDF")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": {"1": []byte(rawPlain), "2": []byte(rawAttached)},
	})
	repo := NewRepository(Config{
		Addr:      srv.addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX"},
		Timeout:   time.Second,
	}, RepositoryOptions{StatePath: t.TempDir() + "/state.json"})

	rows, _, err := repo.SearchPage(context.Background(), "has:attachment", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "attached@example.com" {
		t.Fatalf("rows = %#v, want only attached mail", rows)
	}
	if rows[0].Mailbox != "INBOX" || !rows[0].HasAttachment || rows[0].AttachmentCount != 1 {
		t.Fatalf("attachment metadata = %#v, want INBOX + one attachment", rows[0])
	}
}

func TestRepositorySearchPageSearchesAllMailboxesByDefaultAndNarrowsExplicitInbox(t *testing.T) {
	rawInbox := archiveTestMessage("inbox@example.com", "sender@example.com", "Inbox", "Inbox body.", "")
	rawGmail := archiveTestMessage("gmail@example.com", "sender@example.com", "Gmail", "Gmail body.", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": {"1": []byte(rawInbox)},
		"Gmail": {"2": []byte(rawGmail)},
	})
	repo := NewRepository(Config{
		Addr:      srv.addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX", "Gmail"},
		Timeout:   time.Second,
	}, RepositoryOptions{
		StatePath: t.TempDir() + "/state.json",
		Now:       func() time.Time { return time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC) },
	})

	rows, _, err := repo.SearchPage(context.Background(), "{in:inbox is:unread} newer_than:30d", "", 10)
	if err != nil {
		t.Fatalf("SearchPage default: %v", err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row.ID] = row.Mailbox
	}
	if got["inbox@example.com"] != "INBOX" || got["gmail@example.com"] != "Gmail" {
		t.Fatalf("default rows = %#v, want INBOX and Gmail", rows)
	}

	rows, _, err = repo.SearchPage(context.Background(), "in:inbox newer_than:30d", "", 10)
	if err != nil {
		t.Fatalf("SearchPage inbox: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "inbox@example.com" || rows[0].Mailbox != "INBOX" {
		t.Fatalf("inbox rows = %#v, want only INBOX", rows)
	}

	if err := repo.state.MarkArchived("inbox@example.com"); err != nil {
		t.Fatalf("archive overlay: %v", err)
	}
	rows, _, err = repo.SearchPage(context.Background(), "in:inbox newer_than:30d", "", 10)
	if err != nil {
		t.Fatalf("SearchPage inbox after archive overlay: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("inbox rows after archive overlay = %#v, want hidden", rows)
	}

	rows, _, err = repo.SearchPage(context.Background(), "in:anywhere sender@example.com", "", 10)
	if err != nil {
		t.Fatalf("SearchPage anywhere: %v", err)
	}
	got = map[string]string{}
	for _, row := range rows {
		got[row.ID] = row.Mailbox
	}
	if got["inbox@example.com"] != "INBOX" || got["gmail@example.com"] != "Gmail" {
		t.Fatalf("anywhere rows = %#v, want INBOX and Gmail", rows)
	}
}

func TestRepositorySearchPageScansPastFilteredNewestRows(t *testing.T) {
	msgs := map[string][]byte{}
	for i := 1; i <= 80; i++ {
		uid := fmt.Sprintf("%d", i)
		msgs[uid] = []byte(archiveTestMessage(
			fmt.Sprintf("m%03d@example.com", i),
			"sender@example.com",
			fmt.Sprintf("Message %03d", i),
			"Archive body for native mail.",
			"",
		))
	}
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": msgs,
	})
	repo := NewRepository(Config{
		Addr:      srv.addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX"},
		Timeout:   time.Second,
	}, RepositoryOptions{
		StatePath: t.TempDir() + "/state.json",
		Now:       func() time.Time { return time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC) },
	})
	for i := 51; i <= 80; i++ {
		if err := repo.state.MarkArchived(fmt.Sprintf("m%03d@example.com", i)); err != nil {
			t.Fatalf("mark archived: %v", err)
		}
	}

	rows, next, err := repo.SearchPage(context.Background(), "", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if len(rows) != 10 {
		t.Fatalf("rows len = %d, want a full page after skipping archived newest rows: %#v", len(rows), rows)
	}
	if rows[0].ID != "m050@example.com" || rows[9].ID != "m041@example.com" {
		t.Fatalf("rows = %#v, want newest visible range m050..m041", rows)
	}
	if next == "" {
		t.Fatalf("next is empty, want more unread rows behind the first page")
	}
}

func TestRepositoryMutationResolvesArchiveIDWithoutPriorList(t *testing.T) {
	raw := archiveTestMessage("m3@example.com", "sender@example.com", "Push action", "Opened from notification.", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{
		"INBOX": {"9": []byte(raw)},
	})
	repo := NewRepository(Config{
		Addr:      srv.addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX"},
		Timeout:   time.Second,
	}, RepositoryOptions{StatePath: t.TempDir() + "/state.json"})

	if err := repo.ModifyLabels(context.Background(), "m3@example.com", nil, []string{"UNREAD"}); err != nil {
		t.Fatalf("mark read by Message-ID: %v", err)
	}
	detail, err := repo.GetMessage(context.Background(), "m3@example.com")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if hasLabel(detail.Labels, "UNREAD") {
		t.Fatalf("detail labels = %v, want read overlay", detail.Labels)
	}
	rows, _, err := repo.SearchPage(context.Background(), "{in:inbox is:unread} newer_than:7d", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "m3@example.com" || hasLabel(rows[0].Labels, "UNREAD") {
		t.Fatalf("default rows = %#v, want read mail still visible without UNREAD", rows)
	}
}

func TestRepositoryFallsBackForUnsupportedQuery(t *testing.T) {
	fallback := &fakeRepositoryFallback{
		rows: []gmail.MessageSummary{{ID: "gmail-1", Subject: "starred"}},
	}
	repo := NewRepository(Config{
		Addr:      "127.0.0.1:1",
		User:      "u",
		Pass:      "p",
		Mailboxes: []string{"INBOX"},
		Timeout:   time.Second,
	}, RepositoryOptions{Fallback: fallback})

	rows, _, err := repo.SearchPage(context.Background(), "is:starred", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if !fallback.searchPageCalled {
		t.Fatalf("fallback SearchPage was not called")
	}
	if len(rows) != 1 || rows[0].ID != "gmail-1" {
		t.Fatalf("rows = %#v, want fallback row", rows)
	}
}

// The mail day-pager sends `in:inbox after:D before:D+1`. That must parse natively
// into IMAP SINCE/BEFORE (scoping to exactly that day) instead of being rejected as
// unsupported and falling through to Gmail — the regression that surfaced Gmail rows
// and dropped the per-mail AI analyses keyed by archive Message-IDs.
func TestParseArchiveQueryDateRange(t *testing.T) {
	now := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	spec, err := parseArchiveQuery("in:inbox after:2026/6/30 before:2026/7/1", now)
	if err != nil {
		t.Fatalf("parseArchiveQuery errored (would fall back to Gmail): %v", err)
	}
	if !strings.Contains(spec.Criteria, "SINCE 30-Jun-2026") {
		t.Errorf("criteria %q missing SINCE 30-Jun-2026", spec.Criteria)
	}
	if !strings.Contains(spec.Criteria, "BEFORE 01-Jul-2026") {
		t.Errorf("criteria %q missing BEFORE 01-Jul-2026", spec.Criteria)
	}
	if !spec.InboxOnly {
		t.Errorf("expected InboxOnly for an in:inbox query")
	}
}

type fakeRepositoryFallback struct {
	rows             []gmail.MessageSummary
	searchPageCalled bool
}

func (f *fakeRepositoryFallback) Search(ctx context.Context, query string, maxResults int) ([]gmail.MessageSummary, error) {
	return f.rows, nil
}

func (f *fakeRepositoryFallback) SearchPage(ctx context.Context, query, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error) {
	f.searchPageCalled = true
	return f.rows, "", nil
}

func (f *fakeRepositoryFallback) GetMessage(ctx context.Context, messageID string) (*gmail.MessageDetail, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRepositoryFallback) ModifyLabels(ctx context.Context, messageID string, addNames, removeNames []string) error {
	return nil
}

func (f *fakeRepositoryFallback) Trash(ctx context.Context, messageID string) error {
	return nil
}

func (f *fakeRepositoryFallback) GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

type testIMAPArchive struct {
	addr string
	ln   net.Listener
	msgs map[string]map[string][]byte
}

func newTestIMAPArchive(t *testing.T, msgs map[string]map[string][]byte) *testIMAPArchive {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &testIMAPArchive{addr: ln.Addr().String(), ln: ln, msgs: msgs}
	go srv.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return srv
}

func (s *testIMAPArchive) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *testIMAPArchive) handle(conn net.Conn) {
	defer conn.Close()
	_, _ = fmt.Fprint(conn, "* OK test archive\r\n")
	sc := bufio.NewScanner(conn)
	mailbox := "INBOX"
	for sc.Scan() {
		line := sc.Text()
		tag, cmd, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		upper := strings.ToUpper(cmd)
		switch {
		case strings.HasPrefix(upper, "LOGIN "):
			_, _ = fmt.Fprintf(conn, "%s OK login\r\n", tag)
		case strings.HasPrefix(upper, "EXAMINE "):
			mailbox = unquoteIMAPArg(strings.TrimSpace(cmd[len("EXAMINE "):]))
			_, _ = fmt.Fprintf(conn, "* %d EXISTS\r\n%s OK examine\r\n", len(s.msgs[mailbox]), tag)
		case strings.HasPrefix(upper, "UID SEARCH "):
			uids := s.searchUIDs(mailbox, cmd[len("UID SEARCH "):])
			_, _ = fmt.Fprintf(conn, "* SEARCH %s\r\n%s OK search\r\n", strings.Join(uids, " "), tag)
		case strings.HasPrefix(upper, "UID FETCH "):
			uidSet := strings.Fields(cmd[len("UID FETCH "):])[0]
			for _, uid := range splitUIDSet(uidSet) {
				raw := s.msgs[mailbox][uid]
				if raw == nil {
					continue
				}
				_, _ = fmt.Fprintf(conn, "* 1 FETCH (UID %s BODY[] {%d}\r\n%s)\r\n", uid, len(raw), raw)
			}
			_, _ = fmt.Fprintf(conn, "%s OK fetch\r\n", tag)
		case strings.HasPrefix(upper, "LOGOUT"):
			_, _ = fmt.Fprintf(conn, "* BYE\r\n%s OK logout\r\n", tag)
			return
		default:
			_, _ = fmt.Fprintf(conn, "%s BAD unknown\r\n", tag)
		}
	}
}

func (s *testIMAPArchive) searchUIDs(mailbox, criteria string) []string {
	msgs := s.msgs[mailbox]
	var uids []string
	for uid, raw := range msgs {
		if !testIMAPMessageMatches(raw, criteria) {
			continue
		}
		uids = append(uids, uid)
	}
	sortUIDStrings(uids)
	return uids
}

func testIMAPMessageMatches(raw []byte, criteria string) bool {
	criteria = strings.TrimSpace(criteria)
	upperCriteria := strings.ToUpper(criteria)
	if criteria == "" || strings.EqualFold(criteria, "ALL") || strings.HasPrefix(upperCriteria, "SINCE ") && !strings.Contains(upperCriteria, " OR ") && !strings.Contains(upperCriteria, " FROM ") && !strings.Contains(upperCriteria, " SUBJECT ") && !strings.Contains(upperCriteria, " TEXT ") {
		return true
	}
	rawText := string(raw)
	lowerRaw := strings.ToLower(rawText)
	lowerCriteria := strings.ToLower(criteria)
	if strings.Contains(lowerCriteria, "header ") {
		needle := lastQuotedTerm(criteria)
		if needle == "" {
			return true
		}
		return strings.Contains(rawText, needle) || strings.Contains(lowerRaw, strings.ToLower(needle))
	}
	terms := quotedTerms(criteria)
	if len(terms) == 0 {
		fields := strings.Fields(criteria)
		if len(fields) > 0 {
			terms = append(terms, fields[len(fields)-1])
		}
	}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term != "" && strings.Contains(lowerRaw, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func lastQuotedTerm(s string) string {
	terms := quotedTerms(s)
	if len(terms) == 0 {
		return ""
	}
	return terms[len(terms)-1]
}

func quotedTerms(s string) []string {
	var out []string
	for {
		start := strings.IndexByte(s, '"')
		if start < 0 {
			return out
		}
		s = s[start+1:]
		end := strings.IndexByte(s, '"')
		if end < 0 {
			return out
		}
		out = append(out, strings.ReplaceAll(s[:end], `\"`, `"`))
		s = s[end+1:]
	}
}

func splitUIDSet(uidSet string) []string {
	var out []string
	for _, uid := range strings.Split(uidSet, ",") {
		if uid = strings.TrimSpace(uid); uid != "" {
			out = append(out, uid)
		}
	}
	return out
}

func sortUIDStrings(uids []string) {
	sort.Slice(uids, func(i, j int) bool {
		return parseUID(uids[i]) < parseUID(uids[j])
	})
}

func unquoteIMAPArg(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`)
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

func hasLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}

func archiveTestMessage(messageID, from, subject, body, attachment string) string {
	if attachment == "" {
		return fmt.Sprintf("From: Sender <%s>\r\nTo: User <user@example.com>\r\nSubject: %s\r\nMessage-ID: <%s>\r\nDate: Wed, 17 Jun 2026 11:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", from, subject, messageID, body)
	}
	return fmt.Sprintf("From: Sender <%s>\r\nTo: User <user@example.com>\r\nSubject: %s\r\nMessage-ID: <%s>\r\nDate: Wed, 17 Jun 2026 11:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b\"\r\n\r\n--b\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n--b\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERg==\r\n--b--\r\n", from, subject, messageID, body)
}
