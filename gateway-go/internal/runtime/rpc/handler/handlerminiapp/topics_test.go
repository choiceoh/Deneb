package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// fakeTopicCreator records the last create call so the test can verify
// the active-home chat ID was injected and the name flowed through
// unchanged.
type fakeTopicCreator struct {
	lastChatID    int64
	lastName      string
	lastIconColor int
	resp          *telegram.ForumTopic
	err           error
}

func (f *fakeTopicCreator) CreateForumTopic(_ context.Context, chatID int64, name string, iconColor int) (*telegram.ForumTopic, error) {
	f.lastChatID = chatID
	f.lastName = name
	f.lastIconColor = iconColor
	return f.resp, f.err
}

func newTopicsHandler(creator *fakeTopicCreator, homeChatID int64) func(context.Context, *protocol.RequestFrame) *protocol.ResponseFrame {
	return topicsCreate(TopicsDeps{
		Creator:          func() (TopicCreator, error) { return creator, nil },
		ActiveHomeChatID: func() int64 { return homeChatID },
	})
}

func makeReq(id string, params any) *protocol.RequestFrame {
	raw, _ := json.Marshal(params)
	return &protocol.RequestFrame{ID: id, Method: "miniapp.topics.create", Params: raw}
}

// topicsAuthedCtx satisfies the requireAuth check by attaching the
// sample init data the package's other handler tests use (defined in
// miniapp_test.go). Without it requireAuth fast-returns UNAUTHORIZED
// before we ever exercise the topics-create code paths. Named with the
// topics prefix because gmail_test.go already exports an authedCtx.
func topicsAuthedCtx() context.Context {
	return telegram.WithInitDataContext(context.Background(), sampleInitData())
}

// TestTopicsCreate_HappyPath covers the all-fields-good path: the
// handler injects the active-home chat ID, passes the trimmed name to
// the Bot API, and returns the new MessageThreadID so the Mini App can
// navigate straight into the freshly created topic.
func TestTopicsCreate_HappyPath(t *testing.T) {
	creator := &fakeTopicCreator{
		resp: &telegram.ForumTopic{
			MessageThreadID: 42,
			Name:            "Coding & tech",
			IconColor:       7322096,
		},
	}
	handler := newTopicsHandler(creator, -1001234567890)
	resp := handler(topicsAuthedCtx(), makeReq("1", map[string]any{
		"name":      "  Coding & tech  ", // intentional whitespace
		"iconColor": 7322096,
	}))
	if !resp.OK {
		t.Fatalf("expected OK, got error: %+v", resp.Error)
	}
	if creator.lastChatID != -1001234567890 {
		t.Errorf("chatID injected = %d, want -1001234567890", creator.lastChatID)
	}
	if creator.lastName != "Coding & tech" {
		t.Errorf("name passed = %q, want trimmed 'Coding & tech'", creator.lastName)
	}
	if creator.lastIconColor != 7322096 {
		t.Errorf("iconColor = %d, want 7322096", creator.lastIconColor)
	}
	var out struct {
		MessageThreadID int64  `json:"messageThreadId"`
		Name            string `json:"name"`
		ChatID          int64  `json:"chatId"`
	}
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if out.MessageThreadID != 42 {
		t.Errorf("messageThreadId = %d, want 42", out.MessageThreadID)
	}
	if out.ChatID != -1001234567890 {
		t.Errorf("response chatId = %d, want -1001234567890", out.ChatID)
	}
}

// TestTopicsCreate_NoActiveHome covers the pre-migration state: the
// user runs the Mini App without ever invoking /use-forum first.
// Returns a VALIDATION_FAILED with a redirect hint so the Mini App can
// guide them, instead of confusingly forwarding the create to chat_id=0
// and getting a cryptic Bot API failure.
func TestTopicsCreate_NoActiveHome(t *testing.T) {
	creator := &fakeTopicCreator{}
	handler := newTopicsHandler(creator, 0)
	resp := handler(topicsAuthedCtx(), makeReq("1", map[string]any{"name": "x"}))
	if resp.OK {
		t.Fatalf("expected error for no active home, got OK")
	}
	if resp.Error.Code != protocol.ErrValidationFailed {
		t.Errorf("error code = %q, want VALIDATION_FAILED", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "/use-forum") {
		t.Errorf("error message must hint at /use-forum, got: %q", resp.Error.Message)
	}
	if creator.lastName != "" {
		t.Error("CreateForumTopic should NOT have been called")
	}
}

// TestTopicsCreate_EmptyName covers the trivial guard: a blank or
// whitespace-only name must be rejected at the boundary, not forwarded
// to Telegram (which would also reject but with a less useful error).
func TestTopicsCreate_EmptyName(t *testing.T) {
	creator := &fakeTopicCreator{}
	handler := newTopicsHandler(creator, -1001)
	for _, name := range []string{"", "   ", "\t\n"} {
		resp := handler(topicsAuthedCtx(), makeReq("1", map[string]any{"name": name}))
		if resp.OK {
			t.Errorf("name %q should be rejected, got OK", name)
			continue
		}
		if resp.Error.Code != protocol.ErrMissingParam {
			t.Errorf("name %q: code = %q, want MISSING_PARAM", name, resp.Error.Code)
		}
	}
	if creator.lastName != "" {
		t.Error("creator should never have been called for blank names")
	}
}

// TestTopicsCreate_InvalidIconColor blocks colors outside Telegram's
// six allowed values at our boundary. Catches typos / clipboard
// accidents before they reach the Bot API, where the failure mode is
// a 400 with a less helpful message.
func TestTopicsCreate_InvalidIconColor(t *testing.T) {
	creator := &fakeTopicCreator{}
	handler := newTopicsHandler(creator, -1001)
	resp := handler(topicsAuthedCtx(), makeReq("1", map[string]any{
		"name":      "ok",
		"iconColor": 12345, // not in the allowed set
	}))
	if resp.OK {
		t.Fatalf("expected error for bad iconColor, got OK")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("code = %q, want INVALID_REQUEST", resp.Error.Code)
	}
}

// TestTopicsCreate_BotAPIError covers the post-migration "I demoted
// the bot, now create fails" path. The Bot API's permission error is
// surfaced as DEPENDENCY_FAILED so the Mini App can render the
// upstream message instead of swallowing it.
func TestTopicsCreate_BotAPIError(t *testing.T) {
	creator := &fakeTopicCreator{
		err: errors.New("not enough rights to manage topics"),
	}
	handler := newTopicsHandler(creator, -1001)
	resp := handler(topicsAuthedCtx(), makeReq("1", map[string]any{"name": "ok"}))
	if resp.OK {
		t.Fatalf("expected error, got OK")
	}
	if resp.Error.Code != protocol.ErrDependencyFailed {
		t.Errorf("code = %q, want DEPENDENCY_FAILED", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "not enough rights") {
		t.Errorf("upstream message must be surfaced, got: %q", resp.Error.Message)
	}
}
