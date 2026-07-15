package mailarchive

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestPlanArchiveSearchClampsFetchPerBoxAtBoundary(t *testing.T) {
	spec := archiveQuery{Criteria: "ALL"}
	tests := []struct {
		name      string
		token     string
		pageSize  int
		wantSize  int
		wantFetch int
	}{
		{name: "default page uses minimum scan", wantSize: defaultArchivePageSize, wantFetch: minArchiveFetchPerBox},
		{name: "offset and filter allowance widen scan", token: "archive:50", pageSize: 100, wantSize: 100, wantFetch: 651},
		{name: "deep page is capped", token: "archive:900", pageSize: 100, wantSize: 100, wantFetch: maxArchiveFetchPerBox},
		{name: "oversized page cannot overflow", pageSize: platformMaxInt(), wantSize: platformMaxInt(), wantFetch: maxArchiveFetchPerBox},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := planArchiveSearch([]string{" INBOX ", "INBOX", "Archive"}, spec, tt.token, tt.pageSize)
			if err != nil {
				t.Fatalf("planArchiveSearch: %v", err)
			}
			if plan.pageSize != tt.wantSize || plan.fetchPerBox != tt.wantFetch {
				t.Fatalf("page/fetch = %d/%d, want %d/%d", plan.pageSize, plan.fetchPerBox, tt.wantSize, tt.wantFetch)
			}
			if len(plan.mailboxes) != 2 || plan.mailboxes[0] != "INBOX" || plan.mailboxes[1] != "Archive" {
				t.Fatalf("mailboxes = %#v, want cleaned INBOX+Archive", plan.mailboxes)
			}
		})
	}
}

func TestPlanArchiveSearchRejectsInvalidInputs(t *testing.T) {
	spec := archiveQuery{Criteria: "ALL"}
	if _, err := planArchiveSearch([]string{"INBOX"}, spec, "archive:not-a-number", 10); !errors.Is(err, ErrArchiveUnsupportedQuery) {
		t.Fatalf("malformed token error = %v, want ErrArchiveUnsupportedQuery", err)
	}
	if _, err := planArchiveSearch([]string{" ", "\t"}, spec, "", 10); !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("empty mailbox plan error = %v, want ErrArchiveUnavailable", err)
	}
}

func TestRepositorySearchPageAppliesFiltersAcrossPageBoundary(t *testing.T) {
	messages := map[string][]byte{}
	for uid := 1; uid <= 8; uid++ {
		attachment := ""
		if uid != 3 && uid != 6 {
			attachment = "%PDF"
		}
		messages[strconv.Itoa(uid)] = []byte(archiveTestMessage(
			"filtered-"+strconv.Itoa(uid)+"@example.com",
			"sender@example.com",
			"Filtered "+strconv.Itoa(uid),
			"Archive body.",
			attachment,
		))
	}
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{"INBOX": messages})
	repo := newArchiveSearchTestRepository(t, srv.addr, []string{"INBOX"}, nil)
	if err := repo.state.MarkArchived("filtered-8@example.com"); err != nil {
		t.Fatalf("mark archived: %v", err)
	}
	if err := repo.state.MarkTrashed("filtered-7@example.com"); err != nil {
		t.Fatalf("mark trashed: %v", err)
	}

	first, next, err := repo.SearchPage(context.Background(), "in:inbox has:attachment", "", 2)
	if err != nil {
		t.Fatalf("first SearchPage: %v", err)
	}
	assertArchiveRowIDs(t, first, "filtered-5@example.com", "filtered-4@example.com")
	if next != "archive:2" {
		t.Fatalf("first next = %q, want archive:2", next)
	}

	second, next, err := repo.SearchPage(context.Background(), "in:inbox has:attachment", next, 2)
	if err != nil {
		t.Fatalf("second SearchPage: %v", err)
	}
	assertArchiveRowIDs(t, second, "filtered-2@example.com", "filtered-1@example.com")
	if next != "" {
		t.Fatalf("second next = %q, want empty", next)
	}
}

func TestRepositorySearchPageDegradesToArchiveInsteadOfFallback(t *testing.T) {
	raw := archiveTestMessage("archive-result@example.com", "sender@example.com", "Archive", "Archive body.", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{"INBOX": {"1": []byte(raw)}})
	fallback := &fakeRepositoryFallback{rows: []gmail.MessageSummary{{ID: "fallback-result"}}}
	repo := newArchiveSearchTestRepository(t, srv.addr, []string{"INBOX"}, fallback)

	rows, _, err := repo.SearchPage(context.Background(), "label:work", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	assertArchiveRowIDs(t, rows, "archive-result@example.com")
	if fallback.searchPageCalled {
		t.Fatal("fallback was called for a query that should degrade to the bounded archive view")
	}
}

func TestRepositorySearchPageSkipsFailedMailboxesButErrorsWhenAllFail(t *testing.T) {
	raw := archiveTestMessage("survivor@example.com", "sender@example.com", "Survivor", "Archive body.", "")
	srv := newTestIMAPArchiveRejecting(t, map[string]map[string][]byte{
		"INBOX": {"1": []byte(raw)},
	}, []string{"Broken", "Also Broken"})

	repo := newArchiveSearchTestRepository(t, srv.addr, []string{"Broken", "INBOX"}, nil)
	rows, _, err := repo.SearchPage(context.Background(), "", "", 10)
	if err != nil {
		t.Fatalf("SearchPage with one healthy mailbox: %v", err)
	}
	assertArchiveRowIDs(t, rows, "survivor@example.com")

	repo = newArchiveSearchTestRepository(t, srv.addr, []string{"Broken", "Also Broken"}, nil)
	rows, next, err := repo.SearchPage(context.Background(), "", "", 10)
	if !errors.Is(err, ErrArchiveUnavailable) {
		t.Fatalf("all-mailbox error = %v, want ErrArchiveUnavailable", err)
	}
	if rows != nil || next != "" {
		t.Fatalf("failed rows/next = %#v/%q, want nil/empty", rows, next)
	}
	if !strings.Contains(err.Error(), `examine "Broken"`) || !strings.Contains(err.Error(), `examine "Also Broken"`) {
		t.Fatalf("error %q does not preserve both mailbox failures", err)
	}
}

func TestRepositorySearchPageReturnsEmptyForOversizedPageToken(t *testing.T) {
	raw := archiveTestMessage("one@example.com", "sender@example.com", "One", "Archive body.", "")
	srv := newTestIMAPArchive(t, map[string]map[string][]byte{"INBOX": {"1": []byte(raw)}})
	repo := newArchiveSearchTestRepository(t, srv.addr, []string{"INBOX"}, nil)

	rows, next, err := repo.SearchPage(context.Background(), "", "archive:"+strconv.Itoa(platformMaxInt()), platformMaxInt())
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	if rows != nil || next != "" {
		t.Fatalf("rows/next = %#v/%q, want nil/empty", rows, next)
	}
}

// When Maddy rejects SENTSINCE (e.g. one message with an unparseable ENVELOPE
// Date), the day-pager query must still return Date-header-scoped rows via the
// ALL fallback + post-filter — otherwise Andromeda spins on "불러오는 중…".
func TestSearchPageFallsBackWhenSentSearchRejected(t *testing.T) {
	raw := archiveTestMessage("day@example.com", "sender@example.com", "Day mail", "body", "")
	srv := newTestIMAPArchiveRejectingSentSearch(t, map[string]map[string][]byte{
		"INBOX": {"1": []byte(raw)},
	})
	repo := newArchiveSearchTestRepository(t, srv.addr, []string{"INBOX"}, nil)

	rows, _, err := repo.SearchPage(context.Background(), "in:inbox after:2026/6/17 before:2026/6/18", "", 10)
	if err != nil {
		t.Fatalf("SearchPage: %v", err)
	}
	assertArchiveRowIDs(t, rows, "day@example.com")

	rows, _, err = repo.SearchPage(context.Background(), "in:inbox after:2026/6/16 before:2026/6/17", "", 10)
	if err != nil {
		t.Fatalf("SearchPage other day: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("other day rows = %#v, want empty", rows)
	}
}

func newArchiveSearchTestRepository(t *testing.T, addr string, mailboxes []string, fallback FallbackClient) *Repository {
	t.Helper()
	return NewRepository(Config{
		Addr:      addr,
		User:      "u",
		Pass:      "p",
		Mailboxes: mailboxes,
		Timeout:   time.Second,
	}, RepositoryOptions{
		StatePath: t.TempDir() + "/state.json",
		Fallback:  fallback,
		Now:       func() time.Time { return time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC) },
	})
}

func assertArchiveRowIDs(t *testing.T, rows []gmail.MessageSummary, want ...string) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("row count = %d, want %d: %#v", len(rows), len(want), rows)
	}
	for i, id := range want {
		if rows[i].ID != id {
			t.Fatalf("row %d ID = %q, want %q", i, rows[i].ID, id)
		}
	}
}

func platformMaxInt() int {
	return int(^uint(0) >> 1)
}
