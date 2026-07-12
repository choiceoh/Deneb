package mailtool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailstore"
)

func newArchiveStore(t *testing.T, messages ...mailarchive.ContextMessage) *mailstore.Store {
	t.Helper()
	store, err := mailstore.New(t.TempDir())
	if err != nil {
		t.Fatalf("mailstore.New: %v", err)
	}
	for _, message := range messages {
		if _, err := store.Put(message); err != nil {
			t.Fatalf("store.Put(%q): %v", message.ID, err)
		}
	}
	return store
}

func callArchiveTool(t *testing.T, store *mailstore.Store, input string) (string, error) {
	t.Helper()
	t.Setenv("DENEB_ARCHIVE_IMAP_USER", "")
	t.Setenv("DENEB_ARCHIVE_IMAP_PASS", "")
	t.Setenv("DENEB_ARCHIVE_IMAP_MAILBOXES", "INBOX,Sent")
	return ToolMailArchive(MailArchiveDeps{Store: store})(context.Background(), json.RawMessage(input))
}

func archiveFixture(id, subject, body string, at time.Time) mailarchive.ContextMessage {
	return mailarchive.ContextMessage{
		ID:        id,
		Locator:   "INBOX:" + id,
		Mailbox:   "INBOX",
		UID:       id,
		MessageID: "<" + id + "@example.com>",
		From:      "Sender <sender@example.com>",
		To:        "receiver@example.com",
		Subject:   subject,
		Date:      at.Format(time.RFC1123Z),
		Body:      body,
		Snippet:   body,
	}
}

func TestToolMailArchiveConfigurationAndInputValidation(t *testing.T) {
	t.Setenv("DENEB_ARCHIVE_IMAP_USER", "")
	t.Setenv("DENEB_ARCHIVE_IMAP_PASS", "")

	out, err := ToolMailArchive()(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("unconfigured tool returned error: %v", err)
	}
	if !strings.Contains(out, "설정되지 않았습니다") {
		t.Fatalf("unconfigured output = %q", out)
	}

	store := newArchiveStore(t, archiveFixture("one", "견적 요청", "본문", time.Now()))
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty search", input: `{"action":"search"}`, want: "query가 필요"},
		{name: "empty history", input: `{"action":"project_history"}`, want: "query가 필요"},
		{name: "unknown action", input: `{"action":"explode"}`, want: "알 수 없는 action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callArchiveTool(t, store, tc.input)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}

	out, err = callArchiveTool(t, store, `{"action":"attachment","message_id":"INBOX:one"}`)
	if err != nil {
		t.Fatalf("attachment without IMAP: %v", err)
	}
	if !strings.Contains(out, "IMAP") || !strings.Contains(out, "USER/PASS") {
		t.Fatalf("attachment guidance = %q", out)
	}
}

func TestToolMailArchiveStoreListSearchAndReadJSON(t *testing.T) {
	now := time.Now()
	first := archiveFixture("one", "Alpha 견적", "첫 번째 본문", now.Add(-time.Hour))
	second := archiveFixture("two", "Beta 납기", "두 번째 본문", now)
	store := newArchiveStore(t, first, second)

	list, err := callArchiveTool(t, store, `{"action":"list","days":2,"limit":10,"as_json":true}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed mailArchiveResponse
	if err := json.Unmarshal([]byte(list), &listed); err != nil {
		t.Fatalf("list JSON: %v\n%s", err, list)
	}
	if listed.Action != "list" || listed.Count != 2 || len(listed.Messages) != 2 {
		t.Fatalf("list response = %+v", listed)
	}
	for _, message := range listed.Messages {
		if message.Body != "" {
			t.Fatalf("list without include_body leaked body for %q", message.ID)
		}
	}

	search, err := callArchiveTool(t, store, `{"action":"search","query":"Alpha","as_json":true,"include_body":true}`)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var searched mailArchiveResponse
	if err := json.Unmarshal([]byte(search), &searched); err != nil {
		t.Fatalf("search JSON: %v\n%s", err, search)
	}
	if searched.Count != 1 || searched.Messages[0].ID != "one" || searched.Messages[0].Body != first.Body {
		t.Fatalf("search response = %+v", searched)
	}

	read, err := callArchiveTool(t, store, `{"action":"read","message_id":"INBOX:two","as_json":true}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var opened mailArchiveResponse
	if err := json.Unmarshal([]byte(read), &opened); err != nil {
		t.Fatalf("read JSON: %v\n%s", err, read)
	}
	if opened.Count != 1 || opened.Message == nil || opened.Message.ID != "two" || opened.Message.Body != second.Body {
		t.Fatalf("read response = %+v", opened)
	}
}

func TestToolMailArchiveDispatchCharacterization(t *testing.T) {
	now := time.Now()
	message := archiveFixture("one", "Alpha 견적", "첫 번째 본문", now)
	store := newArchiveStore(t, message)

	// Invalid input historically falls back to the zero-value list action.
	listed, err := callArchiveTool(t, store, `{`)
	if err != nil {
		t.Fatalf("invalid JSON fallback: %v", err)
	}
	if !strings.Contains(listed, "오늘 수신 메일") || !strings.Contains(listed, message.Subject) {
		t.Fatalf("invalid JSON fallback output = %q", listed)
	}

	thread, err := callArchiveTool(t, store, `{"action":"thread","message_id":"INBOX:one","as_json":true}`)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	var threaded mailArchiveResponse
	if err := json.Unmarshal([]byte(thread), &threaded); err != nil {
		t.Fatalf("thread JSON: %v\n%s", err, thread)
	}
	if threaded.Action != "thread" || threaded.Count != 1 || threaded.Messages[0].ID != message.ID {
		t.Fatalf("thread response = %+v", threaded)
	}

	history, err := callArchiveTool(t, store, `{"action":"history","query":"Alpha","as_json":true}`)
	if err != nil {
		t.Fatalf("history alias: %v", err)
	}
	var historical mailArchiveResponse
	if err := json.Unmarshal([]byte(history), &historical); err != nil {
		t.Fatalf("history JSON: %v\n%s", err, history)
	}
	if historical.Action != "project_history" || historical.History == nil {
		t.Fatalf("history response = %+v", historical)
	}
}

func TestMailArchiveScalarHelpers(t *testing.T) {
	if got := mailArchiveMailboxLabel(nil); got != "all" {
		t.Fatalf("mailbox nil = %q", got)
	}
	if got := mailArchiveMailboxLabel([]string{"INBOX", "Sent"}); got != "INBOX+Sent" {
		t.Fatalf("mailbox pair = %q", got)
	}
	if got := mailArchiveBodyRunes(false); got != 2400 {
		t.Fatalf("body runes false = %d", got)
	}
	if got := mailArchiveBodyRunes(true); got != 6000 {
		t.Fatalf("body runes true = %d", got)
	}
	if got := mailArchiveWidenedDays(false, 30); got != 0 {
		t.Fatalf("not widened = %d", got)
	}
	if got := mailArchiveWidenedDays(true, 30); got != 30 {
		t.Fatalf("widened = %d", got)
	}
	if got := minArchiveRelatedMatches(0); got != 0 {
		t.Fatalf("minimum 0 = %d", got)
	}
	if got := minArchiveRelatedMatches(1); got != 1 {
		t.Fatalf("minimum 1 = %d", got)
	}
	if got := minArchiveRelatedMatches(2); got != 2 {
		t.Fatalf("minimum 2 = %d", got)
	}
	if got := minArchiveRelatedMatches(8); got != 2 {
		t.Fatalf("minimum 8 = %d", got)
	}
	if got := formatMailArchiveEventTime(time.Time{}); got != "" {
		t.Fatalf("zero event time = %q", got)
	}
	tm := time.Date(2026, 7, 11, 9, 30, 0, 0, time.FixedZone("KST", 9*60*60))
	if got := formatMailArchiveEventTime(tm); got != tm.Format(time.RFC3339) {
		t.Fatalf("event time = %q", got)
	}
}

func TestArchiveRelatedQueryNormalizesPrefixesAndSender(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  mailarchive.ContextMessage
		want string
	}{
		{name: "plain", msg: mailarchive.ContextMessage{Subject: "  공급 계약  "}, want: "공급 계약"},
		{name: "nested reply", msg: mailarchive.ContextMessage{Subject: "Re: FW: [External] 공급 계약"}, want: "공급 계약"},
		{name: "full width reply", msg: mailarchive.ContextMessage{Subject: "RE： [외부 메일] 일정"}, want: "일정"},
		{name: "parsed sender", msg: mailarchive.ContextMessage{From: `"홍 길동" <hong@example.com>`}, want: "hong@example.com"},
		{name: "raw sender", msg: mailarchive.ContextMessage{From: "not-an-address <"}, want: "not-an-address <"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := archiveRelatedQuery(tc.msg); got != tc.want {
				t.Fatalf("archiveRelatedQuery = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestArchiveRelatedTermsAndEventMatching(t *testing.T) {
	terms := archiveRelatedTerms(`Re: "A" Alpha, 베타! x [납기]`)
	wantTerms := []string{"re", "alpha", "베타", "납기"}
	if strings.Join(terms, "|") != strings.Join(wantTerms, "|") {
		t.Fatalf("terms = %#v, want %#v", terms, wantTerms)
	}

	msg := mailarchive.ContextMessage{
		ID:        "mail-17",
		Locator:   "INBOX:17",
		MessageID: "<message-17@example.com>",
		Subject:   "Alpha 공급 일정",
	}
	for _, tc := range []struct {
		name        string
		source      string
		label       string
		title       string
		description string
		want        bool
	}{
		{name: "id", source: "mail-17", want: true},
		{name: "locator", description: "from INBOX:17", want: true},
		{name: "message id without brackets", label: "message-17@example.com", want: true},
		{name: "full subject", title: "회의: Alpha 공급 일정", want: true},
		{name: "two subject terms", title: "Alpha 일정 검토", want: true},
		{name: "one of three terms", title: "Alpha 검토", want: false},
		{name: "unrelated", title: "Beta 회계", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := archiveEventRelated(tc.source, tc.label, tc.title, tc.description, msg); got != tc.want {
				t.Fatalf("archiveEventRelated = %v, want %v", got, tc.want)
			}
		})
	}

	if archiveEventRelated("", "", "anything", "", mailarchive.ContextMessage{}) {
		t.Fatal("empty message unexpectedly matched")
	}
}

func TestArchiveFormattingContracts(t *testing.T) {
	msg := archiveFixture("one", "Subject\nwith newline", "line one\nline two", time.Now())
	msg.CC = "cc@example.com"
	msg.Score = 1.25
	msg.RankReasons = []string{"subject", "participant"}

	list := formatArchiveMessages("최근 메일", []mailarchive.ContextMessage{msg}, false)
	for _, want := range []string{"최근 메일 — 1건", "Subject with newline", "Locator: INBOX:one", "다음 단계"} {
		if !strings.Contains(list, want) {
			t.Fatalf("list missing %q:\n%s", want, list)
		}
	}
	if strings.Contains(list, "line one\nline two") {
		t.Fatalf("list without body leaked full body:\n%s", list)
	}

	read := formatArchiveRead(msg)
	for _, want := range []string{"## 메일 원문", "**CC:** cc@example.com", "**Message-ID:** <one@example.com>", "**Score:** 1.25", msg.Body} {
		if !strings.Contains(read, want) {
			t.Fatalf("read missing %q:\n%s", want, read)
		}
	}

	empty := msg
	empty.Body = "   "
	if got := formatArchiveRead(empty); !strings.Contains(got, "표시할 본문이 없습니다") {
		t.Fatalf("empty read = %q", got)
	}

	thread := formatArchiveThread([]mailarchive.ContextMessage{msg, empty})
	if !strings.Contains(thread, "전체 메일 스레드 (2개, 오래된 순)") || !strings.Contains(thread, "(본문 없음)") {
		t.Fatalf("thread output:\n%s", thread)
	}
	if got := formatArchiveThread(nil); got != "스레드에 메시지가 없습니다." {
		t.Fatalf("empty thread = %q", got)
	}
	if got := formatArchiveMessages("검색", nil, false); !strings.Contains(got, "해당하는 메일이 없습니다") {
		t.Fatalf("empty messages = %q", got)
	}
}

func TestProjectHistoryAndRelatedSummaryFormatting(t *testing.T) {
	msg := archiveFixture("one", "Alpha", "body", time.Now())
	history := mailarchive.ProjectHistory{
		Query:     "Alpha",
		Messages:  []mailarchive.ContextMessage{msg},
		IndexUsed: true,
		Threads: []mailarchive.ProjectThread{{
			Subject:      "Alpha",
			Count:        2,
			FirstDate:    "2026-07-01",
			LastDate:     "2026-07-11",
			Participants: []string{"a@example.com", "b@example.com"},
			Locators:     []string{"INBOX:old", "INBOX:one"},
		}},
	}
	formatted := formatProjectHistory(history, true)
	for _, want := range []string{"프로젝트 메일 히스토리: Alpha", "로컬 FTS", "참여자: a@example.com, b@example.com", "대표 Locator: INBOX:one", "body"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("history missing %q:\n%s", want, formatted)
		}
	}

	msgs := []mailArchiveMessageOut{
		{
			RelatedWiki:   []mailArchiveWikiHit{{Path: "alpha.md", Snippet: "alpha"}, {Path: "alpha.md", Snippet: "duplicate"}},
			RelatedEvents: []mailArchiveEventHit{{ID: "event-1", Title: "Alpha 회의", Start: "2026-07-11T09:00:00+09:00"}},
		},
		{
			RelatedWiki:   []mailArchiveWikiHit{{Path: "beta.md"}},
			RelatedEvents: []mailArchiveEventHit{{ID: "event-1", Title: "duplicate"}, {Title: "Beta 회의", Start: "tomorrow"}},
		},
	}
	summary := formatMailArchiveRelatedSummary(msgs)
	if strings.Count(summary, "alpha.md") != 1 || strings.Count(summary, "Alpha 회의") != 1 {
		t.Fatalf("summary did not deduplicate:\n%s", summary)
	}
	for _, want := range []string{"beta.md", "Beta 회의", "## 연결된 맥락", "### 위키", "### 일정"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	if got := formatMailArchiveRelatedSummary(nil); got != "" {
		t.Fatalf("empty related summary = %q", got)
	}
}

func TestEnrichArchiveMessageBodyPolicyAndMarshal(t *testing.T) {
	msg := archiveFixture("one", "Alpha", "secret body", time.Now())
	withoutBody := enrichArchiveMessage(context.Background(), MailArchiveDeps{}, msg, false)
	if withoutBody.Body != "" || withoutBody.ID != msg.ID {
		t.Fatalf("without body = %+v", withoutBody)
	}
	withBody := enrichArchiveMessage(context.Background(), MailArchiveDeps{}, msg, true)
	if withBody.Body != msg.Body {
		t.Fatalf("with body = %+v", withBody)
	}

	encoded, err := marshalMailArchiveResponse(mailArchiveResponse{
		Action:    "read",
		Mailboxes: []string{"INBOX"},
		Count:     1,
		Message:   &withBody,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(encoded, "\n  \"action\"") || !strings.Contains(encoded, "secret body") {
		t.Fatalf("marshal output:\n%s", encoded)
	}
}
