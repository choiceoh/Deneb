package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// fakeGmailClient implements GmailClient with function fields so each test
// can wire up exactly the behavior it needs.
type fakeGmailClient struct {
	searchFn       func(ctx context.Context, query string, maxResults int) ([]gmail.MessageSummary, error)
	getMessageFn   func(ctx context.Context, id string) (*gmail.MessageDetail, error)
	modifyLabelsFn func(ctx context.Context, id string, add, remove []string) error
}

func (f *fakeGmailClient) Search(ctx context.Context, q string, n int) ([]gmail.MessageSummary, error) {
	if f.searchFn == nil {
		return nil, errors.New("Search not stubbed")
	}
	return f.searchFn(ctx, q, n)
}

func (f *fakeGmailClient) GetMessage(ctx context.Context, id string) (*gmail.MessageDetail, error) {
	if f.getMessageFn == nil {
		return nil, errors.New("GetMessage not stubbed")
	}
	return f.getMessageFn(ctx, id)
}

func (f *fakeGmailClient) ModifyLabels(ctx context.Context, id string, add, remove []string) error {
	if f.modifyLabelsFn == nil {
		return errors.New("ModifyLabels not stubbed")
	}
	return f.modifyLabelsFn(ctx, id, add, remove)
}

func depsFor(client GmailClient) GmailDeps {
	return GmailDeps{Client: func() (GmailClient, error) { return client, nil }}
}

func authedCtx() context.Context {
	return telegram.WithInitDataContext(context.Background(), &telegram.InitData{
		User: &telegram.WebAppUser{ID: 42, FirstName: "Tester"},
	})
}

func reqWith(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("t-1", method, params)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return req
}

func decode(t *testing.T, resp *protocol.ResponseFrame, dest any) {
	t.Helper()
	if resp == nil {
		t.Fatal("nil response")
	}
	if !resp.OK {
		t.Fatalf("response not OK: code=%s message=%s", resp.Error.Code, resp.Error.Message)
	}
	if err := json.Unmarshal(resp.Payload, dest); err != nil {
		t.Fatalf("decode payload: %v (raw=%s)", err, string(resp.Payload))
	}
}

// --- list_recent ---------------------------------------------------------

func TestGmailListRecent_DefaultsAndShape(t *testing.T) {
	var seenQuery string
	var seenLimit int
	client := &fakeGmailClient{
		searchFn: func(_ context.Context, q string, n int) ([]gmail.MessageSummary, error) {
			seenQuery, seenLimit = q, n
			return []gmail.MessageSummary{
				{
					ID: "m1", ThreadID: "t1",
					From: "Alice <alice@example.com>", Subject: "Hi",
					Snippet: "Hello there", Date: "Mon, 26 May 2026 14:30:00 +0900",
					Labels: []string{"INBOX", "UNREAD"},
				},
			}, nil
		},
	}
	h := gmailListRecent(depsFor(client))

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	decode(t, resp, &got)

	if seenQuery != defaultGmailQuery {
		t.Errorf("query = %q, want %q", seenQuery, defaultGmailQuery)
	}
	if seenLimit != defaultGmailLimit {
		t.Errorf("limit = %d, want %d", seenLimit, defaultGmailLimit)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(got.Messages))
	}
	row := got.Messages[0]
	if row["id"] != "m1" || row["from"] != "Alice <alice@example.com>" || row["isUnread"] != true {
		t.Errorf("row shape unexpected: %+v", row)
	}
	if date, _ := row["date"].(string); date == "" {
		t.Errorf("date missing/empty: %v", row["date"])
	}
}

func TestGmailListRecent_RespectsCustomParams(t *testing.T) {
	var seenQuery string
	var seenLimit int
	client := &fakeGmailClient{
		searchFn: func(_ context.Context, q string, n int) ([]gmail.MessageSummary, error) {
			seenQuery, seenLimit = q, n
			return nil, nil
		},
	}
	h := gmailListRecent(depsFor(client))

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", map[string]any{
		"query": "is:starred", "limit": 5,
	}))
	if !resp.OK {
		t.Fatalf("expected OK, got %+v", resp.Error)
	}
	if seenQuery != "is:starred" {
		t.Errorf("query = %q, want is:starred", seenQuery)
	}
	if seenLimit != 5 {
		t.Errorf("limit = %d, want 5", seenLimit)
	}
}

func TestGmailListRecent_LimitClamp(t *testing.T) {
	var seenLimit int
	client := &fakeGmailClient{
		searchFn: func(_ context.Context, _ string, n int) ([]gmail.MessageSummary, error) {
			seenLimit = n
			return nil, nil
		},
	}
	h := gmailListRecent(depsFor(client))

	h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", map[string]any{"limit": 99999}))
	if seenLimit != maxGmailLimit {
		t.Errorf("limit = %d, want clamped to %d", seenLimit, maxGmailLimit)
	}
}

func TestGmailListRecent_RequiresAuth(t *testing.T) {
	h := gmailListRecent(depsFor(&fakeGmailClient{}))
	resp := h(context.Background(), reqWith(t, "miniapp.gmail.list_recent", nil))
	if resp.OK {
		t.Fatalf("expected unauthorized, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestGmailListRecent_ClientUnavailable(t *testing.T) {
	deps := GmailDeps{
		Client: func() (GmailClient, error) { return nil, errors.New("OAuth not configured") },
	}
	h := gmailListRecent(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnavailable)
	}
}

// --- get -----------------------------------------------------------------

func TestGmailGet_HappyPath(t *testing.T) {
	client := &fakeGmailClient{
		getMessageFn: func(_ context.Context, id string) (*gmail.MessageDetail, error) {
			if id != "m1" {
				t.Errorf("id = %q, want m1", id)
			}
			return &gmail.MessageDetail{
				ID: "m1", ThreadID: "t1",
				From: "Alice", To: "Bob", Subject: "Hi",
				Body: "Hello world", Labels: []string{"INBOX"},
				Attachments: []gmail.AttachmentInfo{
					{Filename: "doc.pdf", MimeType: "application/pdf", AttachmentID: "att1", Size: 12345},
				},
			}, nil
		},
	}
	h := gmailGet(depsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.get", map[string]any{"id": "m1"}))

	var got map[string]any
	decode(t, resp, &got)
	if got["body"] != "Hello world" {
		t.Errorf("body = %v", got["body"])
	}
	if got["bodyTotal"].(float64) != float64(len([]rune("Hello world"))) {
		t.Errorf("bodyTotal = %v", got["bodyTotal"])
	}
	atts, _ := got["attachments"].([]any)
	if len(atts) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(atts))
	}
	att := atts[0].(map[string]any)
	if att["filename"] != "doc.pdf" || att["id"] != "att1" {
		t.Errorf("attachment shape: %+v", att)
	}
}

func TestGmailGet_TruncatesBody(t *testing.T) {
	long := stringsRepeat("가", maxGmailBodyChars+50) // 한글 50자 초과
	client := &fakeGmailClient{
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1", Body: long}, nil
		},
	}
	h := gmailGet(depsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.get", map[string]any{"id": "m1"}))

	var got map[string]any
	decode(t, resp, &got)
	body, _ := got["body"].(string)
	total := int(got["bodyTotal"].(float64))
	if total != maxGmailBodyChars+50 {
		t.Errorf("bodyTotal = %d, want %d", total, maxGmailBodyChars+50)
	}
	if !contains(body, "[truncated") {
		t.Errorf("expected truncation marker, body ends with: %q", lastN(body, 50))
	}
}

func TestGmailGet_MissingID(t *testing.T) {
	h := gmailGet(depsFor(&fakeGmailClient{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.get", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestGmailGet_NotFound(t *testing.T) {
	client := &fakeGmailClient{
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return nil, errors.New("HTTP 404: Not Found")
		},
	}
	h := gmailGet(depsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.get", map[string]any{"id": "missing"}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrNotFound)
	}
}

// --- mark_read / archive --------------------------------------------------

func TestGmailMarkRead_RemovesUnreadLabel(t *testing.T) {
	var seenRemove []string
	client := &fakeGmailClient{
		modifyLabelsFn: func(_ context.Context, _ string, _, remove []string) error {
			seenRemove = remove
			return nil
		},
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1", Labels: []string{"INBOX"}}, nil
		},
	}
	h := gmailMarkRead(depsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.mark_read", map[string]any{"id": "m1"}))

	var got map[string]any
	decode(t, resp, &got)
	if len(seenRemove) != 1 || seenRemove[0] != labelUnread {
		t.Errorf("remove = %v, want [UNREAD]", seenRemove)
	}
	labels, _ := got["labels"].([]any)
	if len(labels) != 1 || labels[0] != "INBOX" {
		t.Errorf("labels after = %v, want [INBOX]", labels)
	}
}

func TestGmailArchive_RemovesInboxLabel(t *testing.T) {
	var seenRemove []string
	client := &fakeGmailClient{
		modifyLabelsFn: func(_ context.Context, _ string, _, remove []string) error {
			seenRemove = remove
			return nil
		},
		getMessageFn: func(_ context.Context, _ string) (*gmail.MessageDetail, error) {
			return &gmail.MessageDetail{ID: "m1", Labels: []string{}}, nil
		},
	}
	h := gmailArchive(depsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.archive", map[string]any{"id": "m1"}))

	if !resp.OK {
		t.Fatalf("expected OK, got %+v", resp.Error)
	}
	if len(seenRemove) != 1 || seenRemove[0] != labelInbox {
		t.Errorf("remove = %v, want [INBOX]", seenRemove)
	}
}

func TestGmailArchive_ModifyFails(t *testing.T) {
	client := &fakeGmailClient{
		modifyLabelsFn: func(_ context.Context, _ string, _, _ []string) error {
			return errors.New("HTTP 403: insufficient permission")
		},
	}
	h := gmailArchive(depsFor(client))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.archive", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrForbidden {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrForbidden)
	}
}

// --- helpers --------------------------------------------------------------

func TestNormalizeDate_RFC2822ToISO8601(t *testing.T) {
	in := "Mon, 26 May 2026 14:30:00 +0900"
	got := normalizeDate(in)
	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("output not RFC3339: %q (err=%v)", got, err)
	}
	if parsed.Hour() != 5 || parsed.Minute() != 30 { // 14:30 KST = 05:30 UTC
		t.Errorf("UTC time = %s, want 05:30:00", parsed.Format("15:04:05"))
	}
}

func TestNormalizeDate_PassthroughOnParseFail(t *testing.T) {
	if got := normalizeDate("garbage"); got != "garbage" {
		t.Errorf("got %q, want passthrough", got)
	}
	if got := normalizeDate(""); got != "" {
		t.Errorf("empty in → got %q", got)
	}
}

func TestHasUnreadLabel(t *testing.T) {
	if !hasUnreadLabel([]string{"INBOX", "UNREAD"}) {
		t.Error("expected true for UNREAD in [INBOX UNREAD]")
	}
	if hasUnreadLabel([]string{"INBOX"}) {
		t.Error("expected false")
	}
	if hasUnreadLabel(nil) {
		t.Error("expected false for nil")
	}
}

func TestGmailMethods_NilClientReturnsNil(t *testing.T) {
	if got := GmailMethods(GmailDeps{Client: nil}); got != nil {
		t.Errorf("GmailMethods(nil client) = %v, want nil", got)
	}
}

func TestGmailMethods_RegistersAllFour(t *testing.T) {
	got := GmailMethods(depsFor(&fakeGmailClient{}))
	for _, name := range []string{
		"miniapp.gmail.list_recent",
		"miniapp.gmail.get",
		"miniapp.gmail.mark_read",
		"miniapp.gmail.archive",
	} {
		if _, ok := got[name]; !ok {
			t.Errorf("missing method %q", name)
		}
	}
}

// --- tiny string utilities (avoiding the deprecated strings.Repeat import dance) ---

func stringsRepeat(s string, n int) string {
	var b []byte
	bs := []byte(s)
	for range n {
		b = append(b, bs...)
	}
	return string(b)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
