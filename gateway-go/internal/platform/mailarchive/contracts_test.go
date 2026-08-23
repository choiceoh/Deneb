package mailarchive

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive/overlay"
)

func TestMailboxSelectionContractMatrix(t *testing.T) {
	tests := []struct {
		name       string
		selector   string
		configured []string
		want       []string
	}{
		{name: "empty defaults", selector: "", want: []string{"INBOX", "Gmail"}},
		{name: "all defaults", selector: "all", want: []string{"INBOX", "Gmail"}},
		{name: "star defaults", selector: "*", want: []string{"INBOX", "Gmail"}},
		{name: "configured all cleaned", selector: "all", configured: []string{" INBOX ", "Archive", "archive", ""}, want: []string{"INBOX", "Archive"}},
		{name: "inbox", selector: " inbox ", configured: []string{"Other"}, want: []string{"INBOX"}},
		{name: "archive configured", selector: "archive", configured: []string{"INBOX", "Archive", "Projects"}, want: []string{"Archive", "Projects"}},
		{name: "backfill configured", selector: "backfill", configured: []string{"INBOX", "Gmail"}, want: []string{"Gmail"}},
		{name: "archive fallback aliases", selector: "archive", configured: []string{"INBOX"}, want: []string{"Archive", "All Mail", "Gmail"}},
		{name: "all mail alias", selector: "all mail", configured: []string{"INBOX", "Archive"}, want: []string{"Archive"}},
		{name: "all-mail alias", selector: "all-mail", configured: []string{"INBOX", "Archive"}, want: []string{"Archive"}},
		{name: "general alias", selector: "general", configured: []string{"INBOX", "Archive"}, want: []string{"Archive"}},
		{name: "mail alias", selector: "mail", configured: []string{"INBOX", "Archive"}, want: []string{"Archive"}},
		{name: "legacy underscore", selector: "legacy_gmail", want: []string{"Gmail"}},
		{name: "legacy hyphen", selector: "legacy-gmail", want: []string{"Gmail"}},
		{name: "gmail", selector: "gmail", want: []string{"Gmail"}},
		{name: "custom preserved", selector: "  Project Mail  ", want: []string{"Project Mail"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SelectMailboxes(tt.selector, tt.configured); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMailboxParsingAndLookupContract(t *testing.T) {
	parseTests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty", raw: "", want: nil},
		{name: "whitespace", raw: "  ", want: nil},
		{name: "single", raw: "INBOX", want: []string{"INBOX"}},
		{name: "trim and dedupe", raw: " INBOX, Archive,archive, Gmail ", want: []string{"INBOX", "Archive", "Gmail"}},
		{name: "empty entries", raw: ",INBOX,,,Gmail,", want: []string{"INBOX", "Gmail"}},
	}
	for _, tt := range parseTests {
		t.Run("parse/"+tt.name, func(t *testing.T) {
			if got := ParseMailboxList(tt.raw); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}

	lookupTests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "custom", in: "Projects", want: []string{"Projects"}},
		{name: "gmail legacy fallback", in: "Gmail", want: []string{"Gmail", "Archive", "All Mail"}},
		{name: "legacy underscore", in: "legacy_gmail", want: []string{"legacy_gmail", "Archive", "All Mail"}},
		{name: "archive legacy fallback", in: "Archive", want: []string{"Archive", "Gmail"}},
		{name: "all mail legacy fallback", in: "All Mail", want: []string{"All Mail", "Gmail"}},
	}
	for _, tt := range lookupTests {
		t.Run("lookup/"+tt.name, func(t *testing.T) {
			if got := lookupMailboxCandidates(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddressFromEnvContract(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_IMAP_ADDR", "")
	if got := AddressFromEnv(); got != defaultAddress {
		t.Fatalf("default = %q", got)
	}
	t.Setenv("DENEB_ARCHIVE_IMAP_ADDR", "  archive.internal:1143  ")
	if got := AddressFromEnv(); got != "archive.internal:1143" {
		t.Fatalf("override = %q", got)
	}
	t.Setenv("DENEB_ARCHIVE_IMAP_ADDR", " \t\n ")
	if got := AddressFromEnv(); got != defaultAddress {
		t.Fatalf("whitespace override = %q", got)
	}
}

func TestContextParticipantsRFCAddressContract(t *testing.T) {
	tests := []struct {
		name string
		msg  ContextMessage
		want []string
	}{
		{
			name: "quoted comma display name remains one participant",
			msg: ContextMessage{
				From: `"Doe, Jane" <jane@example.com>`,
			},
			want: []string{"jane@example.com"},
		},
		{
			name: "address list parses every mailbox",
			msg: ContextMessage{
				To: `Alice <alice@example.com>, "Kim, Min" <kim@example.kr>`,
			},
			want: []string{"alice@example.com", "kim@example.kr"},
		},
		{
			name: "from to cc order and case-insensitive dedupe",
			msg: ContextMessage{
				From: "A@example.com",
				To:   "a@EXAMPLE.com, b@example.com",
				CC:   "c@example.com",
			},
			want: []string{"A@example.com", "b@example.com", "c@example.com"},
		},
		{
			name: "malformed legacy list falls back best effort",
			msg: ContextMessage{
				From: "valid@example.com, not-an-address@@",
			},
			want: []string{"valid@example.com", "not-an-address@@"},
		},
		{
			name: "blank headers",
			msg:  ContextMessage{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contextParticipants(tt.msg); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}

	msg := ContextMessage{From: "a@example.com"}
	for i := 0; i < 12; i++ {
		if i > 0 {
			msg.To += ","
		}
		msg.To += fmt.Sprintf("p%d@example.com", i)
	}
	if got := contextParticipants(msg); len(got) != 8 {
		t.Fatalf("participant cap = %d, want 8: %v", len(got), got)
	}
}

func TestNormalizeProjectSubjectContract(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain lower and spaces", in: "  Alpha   Project  ", want: "alpha project"},
		{name: "reply", in: "Re: Alpha Project", want: "alpha project"},
		{name: "nested reply forward", in: "RE: Fwd: Re: Alpha Project", want: "alpha project"},
		{name: "full width reply colon", in: "Re： Alpha Project", want: "alpha project"},
		{name: "external marker", in: "[외부메일] Re: Alpha Project", want: "alpha project"},
		{name: "spaced external marker", in: "[외부 메일] FW: Alpha Project", want: "alpha project"},
		{name: "english external marker", in: "[EXTERNAL] Fwd: Alpha Project", want: "alpha project"},
		{name: "prefix text elsewhere remains", in: "Alpha Re: Project", want: "alpha re: project"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeProjectSubject(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContextMessageDeduplicateKeyPrefersMessageIDThenIDThenLocator(t *testing.T) {
	tests := []struct {
		name string
		msg  ContextMessage
		want string
	}{
		{name: "message id wins", msg: ContextMessage{MessageID: " <ABC@Example.COM> ", ID: "id", Locator: "loc"}, want: "mid:abc@example.com"},
		{name: "id fallback", msg: ContextMessage{ID: " id-1 ", Locator: "loc"}, want: "id:id-1"},
		{name: "locator fallback", msg: ContextMessage{Locator: "archive:INBOX|1"}, want: "loc:archive:INBOX|1"},
		{name: "empty", msg: ContextMessage{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contextMessageDedupeKey(tt.msg); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilterSentOnOrAfterPreservesSelectedOrder(t *testing.T) {
	loc := time.FixedZone("KST", 9*3600)
	since := time.Date(2026, 7, 3, 12, 0, 0, 0, loc)
	msgs := []ContextMessage{
		{ID: "old", Date: "Thu, 2 Jul 2026 23:59:00 +0900"},
		{ID: "edge", Date: "Fri, 3 Jul 2026 00:00:00 +0900"},
		{ID: "invalid", Date: "not a date"},
		{ID: "new", Date: "Sat, 4 Jul 2026 08:00:00 +0900"},
	}
	got := filterSentOnOrAfter(msgs, since)
	want := []string{"edge", "invalid", "new"}
	ids := make([]string, len(got))
	for i := range got {
		ids[i] = got[i].ID
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}

func TestContextLimitContract(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   int
		want int
	}{
		{name: "negative", in: -1, want: defaultContextLimit},
		{name: "zero", in: 0, want: defaultContextLimit},
		{name: "one", in: 1, want: 1},
		{name: "max", in: maxContextLimit, want: maxContextLimit},
		{name: "over max", in: maxContextLimit + 1, want: maxContextLimit},
	} {
		t.Run("return/"+tt.name, func(t *testing.T) {
			if got := clampContextLimit(tt.in); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		name string
		in   int
		want int
	}{
		{name: "negative", in: -1, want: defaultContextIndexLimit},
		{name: "zero", in: 0, want: defaultContextIndexLimit},
		{name: "one", in: 1, want: 1},
		{name: "max", in: maxContextIndexLimit, want: maxContextIndexLimit},
		{name: "over max", in: maxContextIndexLimit + 1, want: maxContextIndexLimit},
	} {
		t.Run("index/"+tt.name, func(t *testing.T) {
			if got := clampContextIndexLimit(tt.in); got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
	if got := projectHistoryCandidateLimit(1, 0); got != 200 {
		t.Fatalf("small derived candidate limit = %d, want 200", got)
	}
	if got := projectHistoryCandidateLimit(50, 0); got != 400 {
		t.Fatalf("derived candidate limit = %d, want 400", got)
	}
	if got := projectHistoryCandidateLimit(1, 77); got != 77 {
		t.Fatalf("explicit index limit = %d, want 77", got)
	}
}

func TestArchiveTextCriteriaEncodesSpecialCharactersAndMultipleTerms(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "empty still produces one empty criterion", query: "", want: []string{`OR OR FROM "" SUBJECT "" TEXT ""`}},
		{name: "spaces empty", query: "   ", want: []string{`OR OR FROM "   " SUBJECT "   " TEXT "   "`}},
		{name: "quote escaped", query: `alpha"beta`, want: []string{`OR OR FROM "alpha\"beta" SUBJECT "alpha\"beta" TEXT "alpha\"beta"`}},
		{name: "backslash escaped", query: `alpha\beta`, want: []string{`OR OR FROM "alpha\\beta" SUBJECT "alpha\\beta" TEXT "alpha\\beta"`}},
		{name: "multiple terms", query: "alpha beta", want: []string{
			`OR OR FROM "alpha" SUBJECT "alpha" TEXT "alpha"`,
			`OR OR FROM "beta" SUBJECT "beta" TEXT "beta"`,
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := archiveTextCriteria(tt.query)
			if got != strings.Join(tt.want, " ") {
				t.Fatalf("got %q, want %q", got, strings.Join(tt.want, " "))
			}
		})
	}
}

func TestRankTermsAndTermMatchingContract(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "empty", query: "", want: []string{}},
		{name: "lowercase", query: "Alpha BETA", want: []string{"alpha", "beta"}},
		{name: "punctuation trimmed", query: `(alpha), [beta]!`, want: []string{"alpha", "beta"}},
		{name: "single rune dropped", query: "a b c", want: []string{"a b c"}},
		{name: "korean terms", query: "견적 검토", want: []string{"견적", "검토"}},
		{name: "mixed short and long", query: "a alpha 나 견적", want: []string{"alpha", "견적"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rankTerms(tt.query); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
	if allRankTermsMatch("alpha beta", nil) {
		t.Fatal("empty terms matched")
	}
	if !allRankTermsMatch("alpha and beta", []string{"alpha", "beta"}) {
		t.Fatal("all present terms did not match")
	}
	if allRankTermsMatch("alpha only", []string{"alpha", "beta"}) {
		t.Fatal("missing term matched")
	}
}

func TestRankProjectMessagesPreservesInputAndRanksBySignal(t *testing.T) {
	now := time.Now()
	input := []ContextMessage{
		{
			ID:      "plain",
			Locator: "loc-plain",
			From:    "plain@example.com",
			Subject: "Alpha project",
			Body:    "ordinary update",
			when:    now.Add(-20 * 24 * time.Hour),
		},
		{
			ID:        "important",
			Locator:   "loc-important",
			From:      "sales@example.com",
			Subject:   "Alpha project",
			Body:      "견적 금액 1,000만원 납기 마감 검토 필요",
			MessageID: "<important@example.com>",
			References: []string{
				"<root@example.com>",
			},
			Attachments: []gmail.AttachmentInfo{{
				Filename: "alpha-quote.xlsx",
				MimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			}},
			when: now.Add(-time.Hour),
		},
		{
			ID:      "participant",
			Locator: "loc-participant",
			From:    "Alpha Owner <alpha@example.com>",
			Subject: "Unrelated subject",
			Body:    "ordinary",
			when:    now.Add(-2 * time.Hour),
		},
	}
	original := append([]ContextMessage(nil), input...)
	ranked := rankProjectMessages("alpha", input)
	if len(ranked) != len(input) {
		t.Fatalf("ranked len = %d", len(ranked))
	}
	if ranked[0].ID != "important" {
		t.Fatalf("top message = %s, want important: %+v", ranked[0].ID, ranked)
	}
	reasons := strings.Join(ranked[0].RankReasons, ",")
	for _, want := range []string{"subject", "attachment", "attachment_match", "deadline_or_action", "money", "thread_headers", "recent"} {
		if !strings.Contains(reasons, want) {
			t.Errorf("top reasons %q missing %q", reasons, want)
		}
	}
	if input[0].Score != original[0].Score || input[1].Score != original[1].Score || len(input[1].RankReasons) != 0 {
		t.Fatalf("ranking mutated input: before=%+v after=%+v", original, input)
	}
	if got := rankProjectMessages("alpha", nil); got != nil {
		t.Fatalf("nil input = %#v", got)
	}
}

func TestRecencyBoostDecayAtEachAgeBoundary(t *testing.T) {
	now := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		when time.Time
		want float64
	}{
		{name: "future within tolerance", when: now.Add(time.Hour), want: 2.0},
		{name: "future beyond tolerance", when: now.Add(25 * time.Hour), want: 0},
		{name: "now", when: now, want: 2.0},
		{name: "one day", when: now.Add(-24 * time.Hour), want: 2.0},
		{name: "seven days", when: now.Add(-7 * 24 * time.Hour), want: 2.0},
		{name: "thirty days", when: now.Add(-30 * 24 * time.Hour), want: 1.2},
		{name: "older than forty five", when: now.Add(-46 * 24 * time.Hour), want: 0.6},
		{name: "older than one twenty", when: now.Add(-121 * 24 * time.Hour), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recencyBoost(now, tt.when); got != tt.want {
				t.Fatalf("got %.1f, want %.1f", got, tt.want)
			}
		})
	}
}

func TestContextSortingAndReverseContract(t *testing.T) {
	timeA := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	timeB := timeA.Add(time.Hour)
	base := []ContextMessage{
		{ID: "b-uid2", Mailbox: "B", UID: "2", when: timeA},
		{ID: "a-uid10", Mailbox: "A", UID: "10", when: timeA},
		{ID: "a-uid2", Mailbox: "A", UID: "2", when: timeA},
		{ID: "new", Mailbox: "Z", UID: "1", when: timeB},
	}
	chrono := append([]ContextMessage(nil), base...)
	sortContextMessages(chrono, true)
	wantChrono := []string{"a-uid2", "a-uid10", "b-uid2", "new"}
	got := make([]string, len(chrono))
	for i := range chrono {
		got[i] = chrono[i].ID
	}
	if !reflect.DeepEqual(got, wantChrono) {
		t.Fatalf("chronological = %v, want %v", got, wantChrono)
	}
	newest := append([]ContextMessage(nil), base...)
	sortContextMessages(newest, false)
	if newest[0].ID != "new" {
		t.Fatalf("newest first = %+v", newest)
	}
	reverseContextMessages(newest)
	if newest[len(newest)-1].ID != "new" {
		t.Fatalf("reverse = %+v", newest)
	}
	reverseContextMessages(nil)
}

func TestClusterProjectThreadsContract(t *testing.T) {
	msgs := []ContextMessage{
		{Locator: "a1", Subject: "Alpha Project", Date: "2026-07-01", From: "a@example.com"},
		{Locator: "a2", Subject: "Re: Alpha Project", Date: "2026-07-02", From: "b@example.com", To: "a@example.com"},
		{Locator: "b1", Subject: "Beta", Date: "2026-07-03", From: "c@example.com"},
		{Locator: "a3", Subject: "Fwd: Alpha Project", Date: "2026-07-04", From: "d@example.com"},
		{Locator: "a4", Subject: "RE: Alpha Project", Date: "2026-07-05", From: "e@example.com"},
		{Locator: "a5", Subject: "RE: Alpha Project", Date: "2026-07-06", From: "f@example.com"},
		{Locator: "a6", Subject: "RE: Alpha Project", Date: "2026-07-07", From: "g@example.com"},
	}
	threads := clusterProjectThreads(msgs)
	if len(threads) != 2 {
		t.Fatalf("threads = %+v", threads)
	}
	alpha := threads[0]
	if alpha.Key != "alpha project" || alpha.Count != 6 {
		t.Fatalf("alpha = %+v", alpha)
	}
	if alpha.FirstDate != "2026-07-01" || alpha.LastDate != "2026-07-07" {
		t.Fatalf("alpha dates = %+v", alpha)
	}
	if len(alpha.Locators) != 5 {
		t.Fatalf("locator cap = %d, want 5", len(alpha.Locators))
	}
	if len(alpha.Participants) != 6 {
		t.Fatalf("participants = %v", alpha.Participants)
	}
}

func TestArchiveLocatorRoundTripAndInvalidContract(t *testing.T) {
	tests := []struct {
		name    string
		mailbox string
		uid     string
	}{
		{name: "simple", mailbox: "INBOX", uid: "42"},
		{name: "space", mailbox: "All Mail", uid: "7"},
		{name: "unicode", mailbox: "보관함", uid: "메일 9"},
		{name: "separator", mailbox: "A|B", uid: "1|2"},
		{name: "query chars", mailbox: "A+B&C", uid: "x=y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := archiveLocator(tt.mailbox, tt.uid)
			mailbox, uid, ok := archiveLocatorParts(id)
			if !ok || mailbox != tt.mailbox || uid != tt.uid {
				t.Fatalf("round trip = %q/%q/%v from %q", mailbox, uid, ok, id)
			}
		})
	}
	for _, id := range []string{
		"",
		"gmail:INBOX|1",
		archiveLocatorPrefix,
		archiveLocatorPrefix + "INBOX",
		archiveLocatorPrefix + "INBOX|",
		archiveLocatorPrefix + "|1",
		archiveLocatorPrefix + "INBOX|1|2",
		archiveLocatorPrefix + "%zz|1",
	} {
		if mailbox, uid, ok := archiveLocatorParts(id); ok || mailbox != "" || uid != "" {
			t.Errorf("invalid %q parsed as %q/%q/%v", id, mailbox, uid, ok)
		}
	}
}

func TestArchivePageTokenContract(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		want    int
		wantErr bool
	}{
		{name: "empty", token: "", want: 0},
		{name: "spaces", token: "   ", want: 0},
		{name: "zero", token: archivePageTokenPrefix + "0", want: 0},
		{name: "positive", token: archivePageTokenPrefix + "42", want: 42},
		{name: "trimmed", token: "  " + archivePageTokenPrefix + "7  ", want: 7},
		{name: "wrong prefix", token: "page:1", wantErr: true},
		{name: "missing number", token: archivePageTokenPrefix, wantErr: true},
		{name: "negative", token: archivePageTokenPrefix + "-1", wantErr: true},
		{name: "float", token: archivePageTokenPrefix + "1.5", wantErr: true},
		{name: "text", token: archivePageTokenPrefix + "abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArchivePageToken(tt.token)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("got %d/%v, want %d err=%v", got, err, tt.want, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, ErrArchiveUnsupportedQuery) {
				t.Fatalf("error = %v, want ErrArchiveUnsupportedQuery", err)
			}
		})
	}
}

func TestArchiveLabelOverlayContract(t *testing.T) {
	tests := []struct {
		name    string
		mailbox string
		state   overlay.MessageState
		want    []string
	}{
		{name: "fresh inbox", mailbox: "INBOX", want: []string{"INBOX", "UNREAD"}},
		{name: "read inbox", mailbox: "INBOX", state: overlay.MessageState{Read: true}, want: []string{"INBOX"}},
		{name: "archived inbox", mailbox: "INBOX", state: overlay.MessageState{Archived: true}, want: []string{}},
		{name: "trash wins", mailbox: "INBOX", state: overlay.MessageState{Read: true, Archived: true, Trashed: true}, want: []string{"TRASH"}},
		{name: "archive mailbox", mailbox: "Archive", want: []string{}},
		{name: "case insensitive inbox", mailbox: " inbox ", want: []string{"INBOX", "UNREAD"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelsForArchiveMessage(tt.mailbox, tt.state); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestCloneDetailPreservesOriginalAndReturnsNilInput(t *testing.T) {
	original := &gmail.MessageDetail{
		ID:     "id",
		Labels: []string{"INBOX"},
		Attachments: []gmail.AttachmentInfo{{
			Filename: "report.pdf",
		}},
		References: []string{"<root@example.com>"},
	}
	clone := cloneDetail(original)
	if clone == original || !reflect.DeepEqual(clone, original) {
		t.Fatalf("clone = %+v", clone)
	}
	clone.Labels[0] = "TRASH"
	clone.Attachments[0].Filename = "changed.pdf"
	clone.References[0] = "changed"
	if original.Labels[0] != "INBOX" || original.Attachments[0].Filename != "report.pdf" || original.References[0] != "<root@example.com>" {
		t.Fatalf("clone mutation leaked: %+v", original)
	}
	if cloneDetail(nil) != nil {
		t.Fatal("nil detail clone was non-nil")
	}
}

func TestSnippetAndRuneClampContract(t *testing.T) {
	if got := snippetFromBody("  hello\n\n world  "); got != "hello world" {
		t.Fatalf("snippet = %q", got)
	}
	long := strings.Repeat("가", 361)
	got := snippetFromBody(long)
	if len([]rune(strings.TrimSuffix(got, "..."))) != 360 || !strings.HasSuffix(got, "...") {
		t.Fatalf("long snippet rune len = %d suffix=%v", len([]rune(got)), strings.HasSuffix(got, "..."))
	}
	lateEvidence := strings.Repeat("서문 ", 120) + "결재조건은 납품 후 30일입니다" + strings.Repeat(" 부록", 120)
	centered := snippetFromBodyAroundQuery(lateEvidence, "결재조건")
	if !strings.Contains(centered, "결재조건은 납품 후 30일") || len([]rune(centered)) > 366 {
		t.Fatalf("query-centered snippet = %q (runes=%d)", centered, len([]rune(centered)))
	}
	if got := clampRunes("가나다", 3); got != "가나다" {
		t.Fatalf("exact clamp = %q", got)
	}
	if got := clampRunes("가나다", 2); got != "가나…" {
		t.Fatalf("truncated clamp = %q", got)
	}
}

func TestStringSliceHelpersContract(t *testing.T) {
	original := []string{"1", "2", "3", "4"}
	tests := []struct {
		name string
		n    int
		want []string
	}{
		{name: "negative copies all", n: -1, want: []string{"1", "2", "3", "4"}},
		{name: "zero copies all", n: 0, want: []string{"1", "2", "3", "4"}},
		{name: "one", n: 1, want: []string{"4"}},
		{name: "exact", n: 4, want: []string{"1", "2", "3", "4"}},
		{name: "over", n: 8, want: []string{"1", "2", "3", "4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tailStrings(original, tt.n)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			if len(got) > 0 {
				got[0] = "mutated"
				if original[0] == "mutated" || original[len(original)-len(got)] == "mutated" {
					t.Fatal("tailStrings aliased input")
				}
			}
		})
	}
	values := []string{"a", "b", "c", "d"}
	reverseStrings(values)
	if want := []string{"d", "c", "b", "a"}; !reflect.DeepEqual(values, want) {
		t.Fatalf("reverse = %v", values)
	}
	reverseStrings(values)
	if !reflect.DeepEqual(values, []string{"a", "b", "c", "d"}) {
		t.Fatalf("double reverse = %v", values)
	}
}

func TestUIDHelpersContract(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want int
	}{
		{in: "", want: 0},
		{in: " 42 ", want: 42},
		{in: "001", want: 1},
		{in: "bad", want: 0},
		{in: "-1", want: -1},
	} {
		if got := parseUID(tt.in); got != tt.want {
			t.Errorf("parseUID(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
	for _, tt := range []struct {
		name string
		in   []string
		want string
	}{
		{name: "empty", in: nil, want: ""},
		{name: "single", in: []string{" 7 "}, want: "7"},
		{name: "numeric max not lexical", in: []string{"9", "10", "2"}, want: "10"},
		{name: "whitespace", in: []string{" 3", " 12 ", "8"}, want: "12"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := latestUID(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNativeOverlayStatusContract(t *testing.T) {
	snapshot := map[string]overlay.MessageState{
		"fresh":    {},
		"read":     {Read: true},
		"archived": {Read: true, Archived: true},
		"trashed":  {Read: true, Archived: true, Trashed: true},
	}
	got := nativeOverlayStatus(snapshot)
	if got.Messages != 4 || got.Read != 3 || got.Archived != 2 || got.Trashed != 1 {
		t.Fatalf("status = %+v", got)
	}
	if got := nativeOverlayStatus(nil); got != (NativeOverlayStatus{}) {
		t.Fatalf("nil status = %+v", got)
	}
}

func TestIMAPQuoteAndLiteralHelpersContract(t *testing.T) {
	quoteTests := []struct {
		in   string
		want string
	}{
		{in: "", want: `""`},
		{in: "simple", want: `"simple"`},
		{in: `a"b`, want: `"a\"b"`},
		{in: `a\b`, want: `"a\\b"`},
		{in: `a\"b`, want: `"a\\\"b"`},
		{in: "한글", want: `"한글"`},
	}
	for _, tt := range quoteTests {
		if got := quote(tt.in); got != tt.want {
			t.Errorf("quote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	literalTests := []struct {
		name   string
		entry  string
		want   string
		wantOK bool
	}{
		{name: "crlf", entry: "* 1 FETCH (UID 7 BODY[] {5}\r\nhello)\r\n", want: "hello", wantOK: true},
		{name: "lf", entry: "* 1 FETCH (UID 7 BODY[] {5}\nhello)\n", want: "hello", wantOK: true},
		{name: "short announced tolerated", entry: "* 1 FETCH (UID 7 BODY[] {99}\r\nhello", want: "hello", wantOK: true},
		{name: "zero literal", entry: "* 1 FETCH (UID 7 BODY[] {0}\r\n)\r\n", want: ")\r\n", wantOK: true},
		{name: "no marker", entry: "* 1 FETCH (UID 7)", wantOK: false},
	}
	for _, tt := range literalTests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractLiteralPayload([]byte(tt.entry))
			if ok != tt.wantOK || string(got) != tt.want {
				t.Fatalf("got %q/%v, want %q/%v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
	for _, tt := range []struct {
		entry string
		want  string
	}{
		{entry: "* 1 FETCH (UID 42 BODY[] {1}\r\nx)", want: "42"},
		{entry: "* 1 FETCH (uid 7 FLAGS ())", want: "7"},
		{entry: "* 1 FETCH (BODY[] {1}\r\nx)", want: ""},
		{entry: "UID nope", want: ""},
	} {
		if got := extractFetchUID([]byte(tt.entry)); got != tt.want {
			t.Errorf("extractFetchUID(%q) = %q, want %q", tt.entry, got, tt.want)
		}
	}
}

type chunkReader struct {
	chunks [][]byte
	index  int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.index >= len(r.chunks) {
		return 0, io.EOF
	}
	chunk := r.chunks[r.index]
	r.index++
	return copy(p, chunk), nil
}

func TestReadFullContract(t *testing.T) {
	r := bufio.NewReader(&chunkReader{chunks: [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}})
	buf := make([]byte, 6)
	n, err := readFull(r, buf)
	if err != nil || n != 6 || string(buf) != "abcdef" {
		t.Fatalf("read = %d/%q/%v", n, buf, err)
	}
	r = bufio.NewReader(strings.NewReader("abc"))
	buf = make([]byte, 5)
	n, err = readFull(r, buf)
	if !errors.Is(err, io.EOF) || n != 3 || string(buf[:n]) != "abc" {
		t.Fatalf("short read = %d/%q/%v", n, buf[:n], err)
	}
}

func TestIMAPConnectionParsesSearchResultsAndIncrementsTags(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	c := &imapConn{conn: client, r: bufio.NewReader(client)}
	serverDone := make(chan error, 1)
	go func() {
		r := bufio.NewReader(server)
		line, err := r.ReadString('\n')
		if err != nil {
			serverDone <- err
			return
		}
		if line != "a1 UID SEARCH ALL\r\n" {
			serverDone <- fmt.Errorf("command = %q", line)
			return
		}
		_, err = io.WriteString(server, "* SEARCH 1 2 3\r\n* SEARCH 5\r\na1 OK done\r\n")
		serverDone <- err
	}()
	uids, err := c.uidSearch("ALL")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"1", "2", "3", "5"}; !reflect.DeepEqual(uids, want) {
		t.Fatalf("uids = %v, want %v", uids, want)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if c.tag != 1 || c.nextTag() != "a2" || c.nextTag() != "a3" {
		t.Fatalf("tag progression ended at %d", c.tag)
	}
}

func TestIMAPExecLiteralAndStatusContract(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	c := &imapConn{conn: client, r: bufio.NewReader(client)}
	done := make(chan error, 1)
	go func() {
		r := bufio.NewReader(server)
		line, err := r.ReadString('\n')
		if err != nil {
			done <- err
			return
		}
		if line != "a1 UID FETCH 7 (UID BODY.PEEK[])\r\n" {
			done <- fmt.Errorf("command = %q", line)
			return
		}
		_, err = io.WriteString(server, "* 1 FETCH (UID 7 BODY[] {5}\r\nhello)\r\na1 OK complete\r\n")
		done <- err
	}()
	msgs, err := c.uidFetchMessages("7")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].UID != "7" || string(msgs[0].Raw) != "hello" {
		t.Fatalf("messages = %+v", msgs)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if msgs, err := c.uidFetchMessages("  "); err != nil || msgs != nil {
		t.Fatalf("empty uid fetch = %+v/%v", msgs, err)
	}
}

func TestIMAPPreviewFetchIsBoundedAndMarksTruncation(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	c := &imapConn{conn: client, r: bufio.NewReader(client)}
	done := make(chan error, 1)
	go func() {
		r := bufio.NewReader(server)
		line, err := r.ReadString('\n')
		if err != nil {
			done <- err
			return
		}
		if line != "a1 UID FETCH 7 (UID BODY.PEEK[]<0.5>)\r\n" {
			done <- fmt.Errorf("command = %q", line)
			return
		}
		_, err = io.WriteString(server, "* 1 FETCH (UID 7 BODY[]<0> {5}\r\nhello)\r\na1 OK complete\r\n")
		done <- err
	}()
	msgs, err := c.uidFetchMessagePreviews("7", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || !msgs[0].Truncated || string(msgs[0].Raw) != "hello" {
		t.Fatalf("preview messages = %+v", msgs)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestIMAPPreviewRejectsServerLiteralAboveLimit(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	c := &imapConn{conn: client, r: bufio.NewReader(client)}
	go func() {
		r := bufio.NewReader(server)
		_, _ = r.ReadString('\n')
		_, _ = io.WriteString(server, "* 1 FETCH (UID 7 BODY[]<0> {6}\r\nabcdef)\r\na1 OK complete\r\n")
	}()
	if _, err := c.uidFetchMessagePreviews("7", 5); err == nil || !strings.Contains(err.Error(), "exceeds preview limit") {
		t.Fatalf("error = %v, want bounded literal rejection", err)
	}
}

func TestIMAPRejectedStatusesAreActionable(t *testing.T) {
	tests := []struct {
		name    string
		invoke  func(*imapConn) error
		command string
		status  string
		want    string
	}{
		{
			name: "login no",
			invoke: func(c *imapConn) error {
				return c.login("user", "pass")
			},
			command: `LOGIN "user" "pass"`,
			status:  "NO",
			want:    "login rejected",
		},
		{
			name: "examine bad",
			invoke: func(c *imapConn) error {
				return c.examine("Missing")
			},
			command: `EXAMINE "Missing"`,
			status:  "BAD",
			want:    "examine",
		},
		{
			name: "search no",
			invoke: func(c *imapConn) error {
				_, err := c.uidSearch("ALL")
				return err
			},
			command: "UID SEARCH ALL",
			status:  "NO",
			want:    "search rejected",
		},
		{
			name: "fetch no",
			invoke: func(c *imapConn) error {
				_, err := c.uidFetchMessages("1")
				return err
			},
			command: "UID FETCH 1 (UID BODY.PEEK[])",
			status:  "NO",
			want:    "fetch rejected",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			c := &imapConn{conn: client, r: bufio.NewReader(client)}
			done := make(chan error, 1)
			go func() {
				r := bufio.NewReader(server)
				line, err := r.ReadString('\n')
				if err != nil {
					done <- err
					return
				}
				if strings.TrimSpace(strings.TrimPrefix(line, "a1 ")) != tt.command {
					done <- fmt.Errorf("command = %q, want %q", line, tt.command)
					return
				}
				_, err = fmt.Fprintf(server, "a1 %s rejected\r\n", tt.status)
				done <- err
			}()
			err := tt.invoke(c)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDialIMAPGreetingContract(t *testing.T) {
	tests := []struct {
		name     string
		greeting string
		wantErr  bool
	}{
		{name: "ok", greeting: "* OK archive ready\r\n", wantErr: false},
		{name: "preauth is not supported", greeting: "* PREAUTH ready\r\n", wantErr: true},
		{name: "bye", greeting: "* BYE unavailable\r\n", wantErr: true},
		{name: "malformed", greeting: "hello\r\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				_, _ = io.WriteString(conn, tt.greeting)
				_ = conn.Close()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, err := dialIMAP(ctx, listener.Addr().String(), time.Second)
			if conn != nil {
				conn.close()
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tt.wantErr)
			}
			<-done
		})
	}
}

func TestContextIndexSearchContract(t *testing.T) {
	msgs := []ContextMessage{
		{ID: "subject", Subject: "Alpha Project", Body: "ordinary", Locator: "l1"},
		{ID: "body", Subject: "Other", Body: "Alpha appears in body", Locator: "l2"},
		{ID: "attachment", Subject: "Other", Attachments: []gmail.AttachmentInfo{{Filename: "alpha-report.xlsx"}}, Locator: "l3"},
		{ID: "none", Subject: "Other", Body: "unrelated", Locator: "l4"},
	}
	index := newContextIndex(msgs)
	if index == nil {
		t.Fatal("index is nil")
	}
	got := index.Search("alpha", 10)
	if len(got) != 3 {
		t.Fatalf("search results = %+v", got)
	}
	ids := make([]string, len(got))
	for i := range got {
		ids[i] = got[i].ID
	}
	sort.Strings(ids)
	if want := []string{"attachment", "body", "subject"}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	if got := index.Search("", 10); got != nil {
		t.Fatalf("empty query = %+v", got)
	}
	if got := index.Search("alpha", 1); len(got) != 1 {
		t.Fatalf("limit result len = %d", len(got))
	}
	if got := (*ContextIndex)(nil).Search("alpha", 10); got != nil {
		t.Fatalf("nil index = %+v", got)
	}
}
