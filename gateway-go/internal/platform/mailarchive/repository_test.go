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
	if len(rows) != 0 {
		t.Fatalf("default rows after read = %#v, want hidden", rows)
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
	if len(rows) != 0 {
		t.Fatalf("default rows = %#v, want hidden after read overlay", rows)
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
		if strings.Contains(criteria, `HEADER "Message-ID"`) && !strings.Contains(string(raw), strings.Trim(criteria[strings.LastIndex(criteria, " ")+1:], `"`)) {
			continue
		}
		uids = append(uids, uid)
	}
	sortUIDStrings(uids)
	return uids
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
