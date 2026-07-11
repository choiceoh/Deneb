package mailtool

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
)

func TestBoundaryMailArchivePathMatrix(t *testing.T) {
	tests := []struct {
		action   string
		usedIMAP bool
		want     string
	}{
		{action: "", usedIMAP: false, want: "store"},
		{action: "list", usedIMAP: false, want: "store"},
		{action: "search", usedIMAP: false, want: "store"},
		{action: "read", usedIMAP: false, want: "store"},
		{action: "thread", usedIMAP: false, want: "store"},
		{action: "attachment", usedIMAP: false, want: "attachment"},
		{action: "attachment", usedIMAP: true, want: "attachment"},
		{action: "list", usedIMAP: true, want: "imap-fallback"},
		{action: "search", usedIMAP: true, want: "imap-fallback"},
		{action: "ATTACHMENT", usedIMAP: true, want: "imap-fallback"},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s_imap_%v", tt.action, tt.usedIMAP)
		t.Run(name, func(t *testing.T) {
			if got := mailArchivePath(tt.action, tt.usedIMAP); got != tt.want {
				t.Fatalf("mailArchivePath(%q,%v) = %q, want %q", tt.action, tt.usedIMAP, got, tt.want)
			}
		})
	}
}

func TestBoundaryMailboxLabelPreservesConfiguredOrder(t *testing.T) {
	tests := []struct {
		boxes []string
		want  string
	}{
		{boxes: nil, want: "all"},
		{boxes: []string{}, want: "all"},
		{boxes: []string{"INBOX"}, want: "INBOX"},
		{boxes: []string{"INBOX", "Sent"}, want: "INBOX+Sent"},
		{boxes: []string{"Sent", "INBOX", "Archive"}, want: "Sent+INBOX+Archive"},
		{boxes: []string{"", "INBOX"}, want: "+INBOX"},
	}
	for _, tt := range tests {
		if got := mailArchiveMailboxLabel(tt.boxes); got != tt.want {
			t.Fatalf("mailArchiveMailboxLabel(%#v) = %q, want %q", tt.boxes, got, tt.want)
		}
	}
}

func TestBoundaryWidenedDaysBooleanContract(t *testing.T) {
	for _, days := range []int{-30, -1, 0, 1, 7, 30, 365} {
		if got := mailArchiveWidenedDays(false, days); got != 0 {
			t.Fatalf("not widened days=%d returned %d", days, got)
		}
		if got := mailArchiveWidenedDays(true, days); got != days {
			t.Fatalf("widened days=%d returned %d", days, got)
		}
	}
}

func TestBoundaryOneLineWhitespaceMatrix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "spaces trimmed", in: "  text  ", want: "text"},
		{name: "LF replaced", in: "one\ntwo", want: "one two"},
		{name: "CRLF replaced", in: "one\r\ntwo", want: "one two"},
		{name: "multiple newline creates spaces", in: "one\n\ntwo", want: "one  two"},
		{name: "lone CR retained", in: "one\rtwo", want: "one\rtwo"},
		{name: "unicode", in: "  제목 📎\n둘째  ", want: "제목 📎 둘째"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := oneLine(tt.in); got != tt.want {
				t.Fatalf("oneLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBoundaryArchiveRelatedQueryPrefixPeeling(t *testing.T) {
	tests := []struct {
		subject string
		from    string
		want    string
	}{
		{subject: "Subject", want: "Subject"},
		{subject: " re: Subject ", want: "Subject"},
		{subject: "RE: FW: Fwd: Subject", want: "Subject"},
		{subject: "[외부메일] Re： Subject", want: "Subject"},
		{subject: "[외부 메일] [External] FW： Subject", want: "Subject"},
		{subject: "re: ", from: "Sender <sender@example.com>", want: "sender@example.com"},
		{subject: "", from: "raw sender", want: "raw sender"},
		{subject: "Re without colon", want: "Re without colon"},
	}
	for _, tt := range tests {
		msg := mailarchive.ContextMessage{Subject: tt.subject, From: tt.from}
		if got := archiveRelatedQuery(msg); got != tt.want {
			t.Fatalf("archiveRelatedQuery(%q,%q) = %q, want %q", tt.subject, tt.from, got, tt.want)
		}
	}
}

func TestBoundaryArchiveRelatedTermsPunctuationAndRuneFloor(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{in: "", want: []string{}},
		{in: "a b c", want: []string{}},
		{in: "ab cd", want: []string{"ab", "cd"}},
		{in: `"alpha" (beta) [gamma]`, want: []string{"alpha", "beta", "gamma"}},
		{in: "가 나 가나다", want: []string{"가나다"}},
		{in: "ALPHA alpha", want: []string{"alpha", "alpha"}},
		{in: "mail@example.com", want: []string{"mail@example.com"}},
		{in: "<id@example.com>", want: []string{"id@example.com"}},
	}
	for _, tt := range tests {
		got := archiveRelatedTerms(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("archiveRelatedTerms(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestBoundaryMinimumRelatedMatches(t *testing.T) {
	tests := map[int]int{-5: -5, -1: -1, 0: 0, 1: 1, 2: 2, 3: 2, 10: 2, 1000: 2}
	for in, want := range tests {
		if got := minArchiveRelatedMatches(in); got != want {
			t.Fatalf("minArchiveRelatedMatches(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestBoundaryArchiveEventMatchingIdentifierPrecedence(t *testing.T) {
	msg := mailarchive.ContextMessage{
		ID:        "mail-17",
		Locator:   "INBOX:17",
		MessageID: "<message-17@example.com>",
		Subject:   "Quarterly Supply Schedule",
	}
	tests := []struct {
		name        string
		source      string
		label       string
		title       string
		description string
		want        bool
	}{
		{name: "ID exact", source: "mail-17", want: true},
		{name: "ID case insensitive", source: "MAIL-17", want: true},
		{name: "locator", label: "INBOX:17", want: true},
		{name: "bracketed message ID", description: "source <message-17@example.com>", want: true},
		{name: "bare message ID", description: "source message-17@example.com", want: true},
		{name: "full subject", title: "Quarterly Supply Schedule", want: true},
		{name: "two subject terms", title: "Quarterly Supply review", want: true},
		{name: "one of three terms", title: "Quarterly review", want: false},
		{name: "unrelated", title: "Calendar event", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := archiveEventRelated(tt.source, tt.label, tt.title, tt.description, msg)
			if got != tt.want {
				t.Fatalf("archiveEventRelated = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBoundaryArchiveEventEmptySubjectDoesNotFuzzyMatch(t *testing.T) {
	msg := mailarchive.ContextMessage{}
	if archiveEventRelated("anything", "anything", "anything", "anything", msg) {
		t.Fatal("empty message identifiers and subject matched arbitrary event")
	}
}

func TestBoundaryParseArchiveToolDateMatrix(t *testing.T) {
	valid := []string{
		"Fri, 11 Jul 2026 09:30:00 +0900",
		"11 Jul 26 00:30 GMT",
		"Fri, 11 Jul 2026 09:30:00 KST",
	}
	for _, raw := range valid {
		if got := parseArchiveToolDate(raw); got.IsZero() {
			t.Fatalf("valid mail date %q parsed as zero", raw)
		}
	}
	for _, raw := range []string{"", "2026-07-11", "not a date", "32 Jul 2026"} {
		if got := parseArchiveToolDate(raw); !got.IsZero() {
			t.Fatalf("invalid mail date %q = %s", raw, got)
		}
	}
}

func TestBoundaryEnrichArchiveMessagesBodyVisibilityAndOrder(t *testing.T) {
	msgs := []mailarchive.ContextMessage{
		{ID: "one", Subject: "First", Body: "first body"},
		{ID: "two", Subject: "Second", Body: "second body"},
		{ID: "three", Subject: "Third", Body: "third body"},
	}
	hidden := enrichArchiveMessages(context.Background(), MailArchiveDeps{}, msgs, false)
	if len(hidden) != len(msgs) {
		t.Fatalf("hidden length = %d", len(hidden))
	}
	for i := range hidden {
		if hidden[i].ID != msgs[i].ID || hidden[i].Body != "" {
			t.Fatalf("hidden[%d] = %#v", i, hidden[i])
		}
	}
	visible := enrichArchiveMessages(context.Background(), MailArchiveDeps{}, msgs, true)
	for i := range visible {
		if visible[i].ID != msgs[i].ID || visible[i].Body != msgs[i].Body {
			t.Fatalf("visible[%d] = %#v", i, visible[i])
		}
	}
	if msgs[0].Body != "first body" {
		t.Fatal("enrichment mutated caller-owned message slice")
	}
}

func TestBoundaryMarshalMailArchiveResponseOmitEmptyContract(t *testing.T) {
	resp := mailArchiveResponse{Action: "list", Mailboxes: []string{"INBOX"}, Count: 0}
	raw, err := marshalMailArchiveResponse(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"action", "mailboxes", "count"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("required key %q missing: %s", key, raw)
		}
	}
	for _, key := range []string{"widened_days", "message", "messages", "history"} {
		if _, ok := decoded[key]; ok {
			t.Fatalf("empty optional key %q present: %s", key, raw)
		}
	}
}

func TestBoundaryFormatArchiveMessagesEmptyAndBodyToggle(t *testing.T) {
	if got := formatArchiveMessages("검색", nil, false); got != "검색: 해당하는 메일이 없습니다." {
		t.Fatalf("empty format = %q", got)
	}
	msg := mailarchive.ContextMessage{ID: "one", Locator: "INBOX:one", Subject: "Subject", From: "Sender", Date: "Date", Snippet: "Snippet", Body: "SECRET BODY"}
	hidden := formatArchiveMessages("검색", []mailarchive.ContextMessage{msg}, false)
	if strings.Contains(hidden, msg.Body) {
		t.Fatal("includeBody=false leaked body")
	}
	visible := formatArchiveMessages("검색", []mailarchive.ContextMessage{msg}, true)
	if !strings.Contains(visible, msg.Body) {
		t.Fatal("includeBody=true omitted body")
	}
}

func TestBoundaryFormatMailArchiveEventTimeRFC3339(t *testing.T) {
	if got := formatMailArchiveEventTime(time.Time{}); got != "" {
		t.Fatalf("zero time = %q", got)
	}
	loc := time.FixedZone("KST", 9*60*60)
	tm := time.Date(2026, 7, 11, 9, 30, 45, 123, loc)
	if got := formatMailArchiveEventTime(tm); got != tm.Format(time.RFC3339) {
		t.Fatalf("event time = %q", got)
	}
}
