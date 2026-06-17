package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// fakeGmailClient implements GmailClient with function fields so each test
// can wire up exactly the behavior it needs.
type fakeGmailClient struct {
	searchFn       func(ctx context.Context, query string, maxResults int) ([]gmail.MessageSummary, error)
	searchPageFn   func(ctx context.Context, query, pageToken string, maxResults int) ([]gmail.MessageSummary, string, error)
	getMessageFn   func(ctx context.Context, id string) (*gmail.MessageDetail, error)
	modifyLabelsFn func(ctx context.Context, id string, add, remove []string) error
	trashFn        func(ctx context.Context, id string) error
}

type fakeNativeStatusClient struct {
	fakeGmailClient
	status mailarchive.NativeStatus
	err    error
}

func (f *fakeNativeStatusClient) NativeStatus(ctx context.Context) (mailarchive.NativeStatus, error) {
	return f.status, f.err
}

func (f *fakeGmailClient) Search(ctx context.Context, q string, n int) ([]gmail.MessageSummary, error) {
	if f.searchFn == nil {
		return nil, errors.New("Search not stubbed")
	}
	return f.searchFn(ctx, q, n)
}

// SearchPage delegates to searchPageFn when set; otherwise it falls back
// to searchFn with the pageToken ignored, which lets the existing
// list_recent tests (which only stub searchFn) continue to exercise the
// handler without each having to plumb an empty nextPageToken. The
// fallback panics on a non-empty pageToken so a future regression where
// the handler stops forwarding p.PageToken can't silently slip past
// tests stubbing only searchFn.
func (f *fakeGmailClient) SearchPage(ctx context.Context, q, pageToken string, n int) ([]gmail.MessageSummary, string, error) {
	if f.searchPageFn != nil {
		return f.searchPageFn(ctx, q, pageToken, n)
	}
	if f.searchFn != nil {
		if pageToken != "" {
			panic("fakeGmailClient: searchFn fallback called with non-empty pageToken; stub searchPageFn explicitly to test pagination")
		}
		out, err := f.searchFn(ctx, q, n)
		return out, "", err
	}
	return nil, "", errors.New("SearchPage not stubbed")
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

func (f *fakeGmailClient) Trash(ctx context.Context, id string) error {
	if f.trashFn == nil {
		return errors.New("Trash not stubbed")
	}
	return f.trashFn(ctx, id)
}

func depsFor(client GmailClient) GmailDeps {
	return GmailDeps{Client: func() (GmailClient, error) { return client, nil }}
}

func authedCtx() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{
		User: &clientauth.User{ID: 42, FirstName: "Tester"},
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
	h := gmailListRecent(depsFor(client), nil)

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

func TestGmailListRecent_NilLabelsSerializeAsEmptyArray(t *testing.T) {
	client := &fakeGmailClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return []gmail.MessageSummary{{ID: "m1", Subject: "No labels"}}, nil
		},
	}
	h := gmailListRecent(depsFor(client), nil)

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	if resp == nil || !resp.OK {
		t.Fatalf("response not OK: %+v", resp)
	}
	raw := string(resp.Payload)
	if strings.Contains(raw, `"labels":null`) {
		t.Fatalf("payload contains labels:null: %s", raw)
	}
	if !strings.Contains(raw, `"labels":[]`) {
		t.Fatalf("payload missing empty labels array: %s", raw)
	}
}

func TestGmailListRecent_PriorityFields(t *testing.T) {
	client := &fakeGmailClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return []gmail.MessageSummary{
				{ID: "m1", From: "a <a@b.kr>", Subject: "낙찰 통보 공문", Snippet: "내일까지 회신 요망"},
				{ID: "m2", From: "b <b@c.kr>", Subject: "안부", Snippet: "잘 지내시죠"},
			}, nil
		},
	}
	deps := depsFor(client)
	deps.Priority = func(from, subject, snippet string) (string, string) {
		if subject == "낙찰 통보 공문" {
			return "urgent", "낙찰 · 마감 표현"
		}
		return "", ""
	}
	h := gmailListRecent(deps, nil)

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	decode(t, resp, &got)
	if len(got.Messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got.Messages))
	}
	if got.Messages[0]["priority"] != "urgent" || got.Messages[0]["priorityHint"] != "낙찰 · 마감 표현" {
		t.Errorf("urgent row missing priority fields: %+v", got.Messages[0])
	}
	// Routine rows omit the fields entirely (omitempty) — the wire stays
	// byte-identical to the pre-priority shape for unmarked mail.
	if _, ok := got.Messages[1]["priority"]; ok {
		t.Errorf("routine row must omit priority: %+v", got.Messages[1])
	}
}

// Layered priority: a cached LLM analysis verdict beats the heuristic —
// urgent/attention render with the "분석 판정" hint, routine suppresses a
// heuristic that would otherwise mark, and a cache miss falls back.
func TestGmailListRecent_AnalysisVerdictLayering(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())
	mustSave := func(id, importance string) {
		t.Helper()
		if err := store.SaveAnalysis(CachedAnalysis{MsgID: id, Analysis: "분석", Importance: importance}); err != nil {
			t.Fatal(err)
		}
	}
	mustSave("m-urgent", "urgent")
	mustSave("m-routine", "routine") // body-aware FYI verdict
	mustSave("m-blank", "")          // v2 record without parseable tag

	client := &fakeGmailClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return []gmail.MessageSummary{
				{ID: "m-urgent", From: "a <a@b.kr>", Subject: "안부"},        // heuristic would say none
				{ID: "m-routine", From: "b <b@c.kr>", Subject: "긴급 낙찰 공문"}, // heuristic would say urgent
				{ID: "m-blank", From: "c <c@d.kr>", Subject: "견적 송부"},      // falls back to heuristic
				{ID: "m-miss", From: "d <d@e.kr>", Subject: "회의 일정 조율"},    // no cache → heuristic
			}, nil
		},
	}
	deps := depsFor(client)
	deps.AnalysisCache = store
	deps.Priority = func(_, subject, _ string) (string, string) {
		switch subject {
		case "긴급 낙찰 공문":
			return "urgent", "낙찰"
		case "견적 송부":
			return "attention", "견적"
		case "회의 일정 조율":
			return "attention", "회의"
		}
		return "", ""
	}
	h := gmailListRecent(deps, nil)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	decode(t, resp, &got)
	byID := map[string]map[string]any{}
	for _, m := range got.Messages {
		byID[m["id"].(string)] = m
	}

	if byID["m-urgent"]["priority"] != "urgent" || byID["m-urgent"]["priorityHint"] != "분석 판정" {
		t.Errorf("analysis urgent must override heuristic-none: %+v", byID["m-urgent"])
	}
	if _, ok := byID["m-routine"]["priority"]; ok {
		t.Errorf("analysis routine must suppress heuristic urgent: %+v", byID["m-routine"])
	}
	if byID["m-blank"]["priority"] != "attention" {
		t.Errorf("blank verdict must fall back to heuristic: %+v", byID["m-blank"])
	}
	if byID["m-miss"]["priority"] != "attention" || byID["m-miss"]["priorityHint"] != "회의" {
		t.Errorf("cache miss must fall back to heuristic: %+v", byID["m-miss"])
	}
}

// Nil Priority dep (tests, wiring without scorer) leaves every row unmarked.
func TestGmailListRecent_NilPriorityDep(t *testing.T) {
	client := &fakeGmailClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return []gmail.MessageSummary{{ID: "m1", From: "a <a@b.kr>", Subject: "긴급 낙찰"}}, nil
		},
	}
	h := gmailListRecent(depsFor(client), nil)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	var got struct {
		Messages []map[string]any `json:"messages"`
	}
	decode(t, resp, &got)
	if _, ok := got.Messages[0]["priority"]; ok {
		t.Errorf("nil dep must not mark rows: %+v", got.Messages[0])
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
	h := gmailListRecent(depsFor(client), nil)

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

func TestGmailListRecent_AbsorbsEmptyPages(t *testing.T) {
	// Gmail can return empty pages with a continuation token (filter
	// pruned the chunk). The handler should hop forward until it gets
	// at least one message, otherwise the Mini App sees an empty
	// state with hidden mail behind it.
	var calls int
	tokens := []string{"", "tok1", "tok2"}
	client := &fakeGmailClient{
		searchPageFn: func(_ context.Context, _, pageToken string, _ int) ([]gmail.MessageSummary, string, error) {
			if calls >= len(tokens) {
				t.Fatalf("unexpected call %d with token %q", calls, pageToken)
			}
			if pageToken != tokens[calls] {
				t.Errorf("call %d: pageToken = %q, want %q", calls, pageToken, tokens[calls])
			}
			calls++
			switch calls {
			case 1: // first call: empty + tok1
				return nil, "tok1", nil
			case 2: // second call: empty + tok2
				return nil, "tok2", nil
			case 3: // third call: non-empty + ""
				return []gmail.MessageSummary{{ID: "m1"}}, "", nil
			}
			return nil, "", nil
		},
	}
	h := gmailListRecent(depsFor(client), nil)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))

	var got struct {
		Messages      []map[string]any `json:"messages"`
		NextPageToken string           `json:"nextPageToken"`
	}
	decode(t, resp, &got)

	if calls != 3 {
		t.Errorf("calls = %d, want 3 (initial + 2 empty-page hops)", calls)
	}
	if len(got.Messages) != 1 || got.Messages[0]["id"] != "m1" {
		t.Errorf("messages = %v, want [{id: m1}]", got.Messages)
	}
	if got.NextPageToken != "" {
		t.Errorf("nextPageToken = %q, want empty", got.NextPageToken)
	}
}

func TestGmailListRecent_EmptyPageHopBudget(t *testing.T) {
	// If Gmail keeps returning empty pages with tokens past the loop
	// budget, the handler should give up gracefully (no infinite
	// loop) and return whatever's at the cutoff.
	var calls int
	client := &fakeGmailClient{
		searchPageFn: func(_ context.Context, _, _ string, _ int) ([]gmail.MessageSummary, string, error) {
			calls++
			return nil, "always-more", nil
		},
	}
	h := gmailListRecent(depsFor(client), nil)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", nil))
	if !resp.OK {
		t.Fatalf("expected OK, got %+v", resp.Error)
	}
	// Budget: initial call + maxEmptyPageHops follow-ups.
	wantCalls := 1 + maxEmptyPageHops
	if calls != wantCalls {
		t.Errorf("calls = %d, want %d (initial + %d hops)", calls, wantCalls, maxEmptyPageHops)
	}
}

func TestMapGmailError_400MapsToInvalidParams(t *testing.T) {
	// A stale/garbled pageToken makes Gmail return 400 — we want the
	// Mini App to see INVALID_REQUEST (reset-to-first-page) rather
	// than UNAVAILABLE (retry, which would loop on the bad token).
	resp := mapGmailError("rid-1", "gmail search failed", errors.New("Gmail API 오류 (HTTP 400): invalid pageToken"))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrInvalidRequest)
	}
}

func TestGmailListRecent_PageTokenRoundTrip(t *testing.T) {
	var seenPageToken string
	client := &fakeGmailClient{
		searchPageFn: func(_ context.Context, _, pageToken string, _ int) ([]gmail.MessageSummary, string, error) {
			seenPageToken = pageToken
			return []gmail.MessageSummary{{ID: "m2"}}, "next-page-abc", nil
		},
	}
	h := gmailListRecent(depsFor(client), nil)

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", map[string]any{
		"pageToken": "incoming-token-xyz",
	}))
	var got struct {
		Messages      []map[string]any `json:"messages"`
		NextPageToken string           `json:"nextPageToken"`
	}
	decode(t, resp, &got)

	if seenPageToken != "incoming-token-xyz" {
		t.Errorf("pageToken sent to client = %q, want %q", seenPageToken, "incoming-token-xyz")
	}
	if got.NextPageToken != "next-page-abc" {
		t.Errorf("nextPageToken in response = %q, want %q", got.NextPageToken, "next-page-abc")
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
	h := gmailListRecent(depsFor(client), nil)

	h(authedCtx(), reqWith(t, "miniapp.gmail.list_recent", map[string]any{"limit": 99999}))
	if seenLimit != maxGmailLimit {
		t.Errorf("limit = %d, want clamped to %d", seenLimit, maxGmailLimit)
	}
}

func TestGmailListRecent_RequiresAuth(t *testing.T) {
	h := gmailListRecent(depsFor(&fakeGmailClient{}), nil)
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
	h := gmailListRecent(deps, nil)
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
	h := gmailArchive(depsFor(client), nil)
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
	h := gmailArchive(depsFor(client), nil)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.archive", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrForbidden {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrForbidden)
	}
}

// --- trash ---------------------------------------------------------------

func TestGmailTrash_CallsClientTrash(t *testing.T) {
	var seenID string
	client := &fakeGmailClient{
		trashFn: func(_ context.Context, id string) error {
			seenID = id
			return nil
		},
	}
	h := gmailTrash(depsFor(client), nil)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.trash", map[string]any{"id": "m1"}))

	var got map[string]any
	decode(t, resp, &got)
	if seenID != "m1" {
		t.Errorf("id = %q, want m1", seenID)
	}
	if got["ok"] != true {
		t.Errorf("ok = %v, want true", got["ok"])
	}
}

func TestGmailTrash_MissingID(t *testing.T) {
	h := gmailTrash(depsFor(&fakeGmailClient{}), nil)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.trash", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestGmailTrash_RequiresAuth(t *testing.T) {
	h := gmailTrash(depsFor(&fakeGmailClient{}), nil)
	resp := h(context.Background(), reqWith(t, "miniapp.gmail.trash", map[string]any{"id": "m1"}))
	if resp.OK {
		t.Fatalf("expected unauthorized, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestGmailTrash_ClientError(t *testing.T) {
	client := &fakeGmailClient{
		trashFn: func(_ context.Context, _ string) error {
			return errors.New("HTTP 404: Not Found")
		},
	}
	h := gmailTrash(depsFor(client), nil)
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.trash", map[string]any{"id": "missing"}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrNotFound)
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

func TestGmailMethods_RegistersAll(t *testing.T) {
	got := GmailMethods(depsFor(&fakeGmailClient{}))
	for _, name := range []string{
		"miniapp.gmail.list_recent",
		"miniapp.gmail.get",
		"miniapp.gmail.mark_read",
		"miniapp.gmail.archive",
		"miniapp.gmail.trash",
		"miniapp.gmail.native_status",
	} {
		if _, ok := got[name]; !ok {
			t.Errorf("missing method %q", name)
		}
	}
}

func TestGmailNativeStatus_ArchiveClient(t *testing.T) {
	h := gmailNativeStatus(depsFor(&fakeNativeStatusClient{
		status: mailarchive.NativeStatus{
			Source:         "archive",
			Available:      true,
			OfflineCapable: true,
			GeneratedAt:    time.Date(2026, 6, 17, 1, 2, 3, 0, time.UTC),
			Mailboxes: []mailarchive.NativeMailboxStatus{{
				Name:              "INBOX",
				Total:             10,
				Unread:            3,
				LocallyRead:       2,
				LatestUID:         "55",
				AttachmentCapable: true,
			}},
			Overlay: mailarchive.NativeOverlayStatus{Messages: 4, Read: 2, Archived: 1, Trashed: 1},
		},
	}))

	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.native_status", nil))
	var got mailNativeStatusOut
	decode(t, resp, &got)
	if got.Source != "archive" || !got.Available || !got.OfflineCapable || got.GeneratedAt != "2026-06-17T01:02:03Z" {
		t.Fatalf("status = %#v", got)
	}
	if len(got.Mailboxes) != 1 || got.Mailboxes[0].Name != "INBOX" || got.Mailboxes[0].Unread != 3 || got.Mailboxes[0].LatestUID != "55" {
		t.Fatalf("mailboxes = %#v", got.Mailboxes)
	}
	if got.Overlay.Messages != 4 || got.Overlay.Archived != 1 {
		t.Fatalf("overlay = %#v", got.Overlay)
	}
}

func TestGmailNativeStatus_GmailFallbackClient(t *testing.T) {
	h := gmailNativeStatus(depsFor(&fakeGmailClient{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.gmail.native_status", nil))
	var got mailNativeStatusOut
	decode(t, resp, &got)
	if got.Source != "gmail" || !got.Available || got.OfflineCapable {
		t.Fatalf("status = %#v, want gmail available without offline capability", got)
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
