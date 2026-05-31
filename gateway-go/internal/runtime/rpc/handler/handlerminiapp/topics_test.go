package handlerminiapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeTopicsClient struct {
	createFn func(ctx context.Context, chatID int64, name string, iconColor int64) (*telegram.ForumTopic, error)
}

func (f *fakeTopicsClient) CreateForumTopic(ctx context.Context, chatID int64, name string, iconColor int64) (*telegram.ForumTopic, error) {
	if f.createFn == nil {
		return &telegram.ForumTopic{MessageThreadID: 1, Name: name, IconColor: iconColor}, nil
	}
	return f.createFn(ctx, chatID, name, iconColor)
}

func topicsDepsFor(client TopicsClient, chatID int64) TopicsDeps {
	return TopicsDeps{
		Client:       func() (TopicsClient, error) { return client, nil },
		ActiveChatID: func() int64 { return chatID },
	}
}

func TestTopicsCreate_HappyPath(t *testing.T) {
	var (
		gotChatID int64
		gotName   string
		gotIcon   int64
	)
	client := &fakeTopicsClient{
		createFn: func(_ context.Context, chatID int64, name string, iconColor int64) (*telegram.ForumTopic, error) {
			gotChatID, gotName, gotIcon = chatID, name, iconColor
			return &telegram.ForumTopic{MessageThreadID: 42, Name: name, IconColor: iconColor}, nil
		},
	}
	h := topicsCreate(topicsDepsFor(client, -1001234567890))
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.create", map[string]any{
		"name":      "  주간 회고  ",
		"iconColor": 7322096,
	}))
	var got struct {
		ThreadID  int64  `json:"threadId"`
		Name      string `json:"name"`
		IconColor int64  `json:"iconColor"`
	}
	decode(t, resp, &got)
	if got.ThreadID != 42 || got.Name != "주간 회고" || got.IconColor != 7322096 {
		t.Errorf("unexpected response: %+v", got)
	}
	if gotChatID != -1001234567890 {
		t.Errorf("chatID = %d, want -1001234567890", gotChatID)
	}
	if gotName != "주간 회고" {
		t.Errorf("name = %q, want trimmed Korean", gotName)
	}
	if gotIcon != 7322096 {
		t.Errorf("iconColor = %d, want 7322096", gotIcon)
	}
}

func TestTopicsCreate_RequiresAuth(t *testing.T) {
	h := topicsCreate(topicsDepsFor(&fakeTopicsClient{}, -100))
	resp := h(context.Background(), reqWith(t, "miniapp.topics.create", map[string]any{"name": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("auth not enforced: %+v", resp)
	}
}

func TestTopicsCreate_MissingName(t *testing.T) {
	h := topicsCreate(topicsDepsFor(&fakeTopicsClient{}, -100))
	for _, name := range []string{"", "   ", "\t\n"} {
		resp := h(authedCtx(), reqWith(t, "miniapp.topics.create", map[string]any{"name": name}))
		if resp.OK || resp.Error.Code != protocol.ErrMissingParam {
			t.Errorf("name=%q: expected MISSING_PARAM, got %+v", name, resp)
		}
	}
}

func TestTopicsCreate_NameTooLong(t *testing.T) {
	long := strings.Repeat("가", 129)
	h := topicsCreate(topicsDepsFor(&fakeTopicsClient{}, -100))
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.create", map[string]any{"name": long}))
	if resp.OK || resp.Error.Code != protocol.ErrValidationFailed {
		t.Errorf("expected VALIDATION_FAILED for 129-rune name, got %+v", resp)
	}
}

func TestTopicsCreate_NoActiveHome(t *testing.T) {
	h := topicsCreate(topicsDepsFor(&fakeTopicsClient{}, 0))
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.create", map[string]any{"name": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE without active home, got %+v", resp)
	}
}

func TestTopicsCreate_DirectChatRejected(t *testing.T) {
	h := topicsCreate(topicsDepsFor(&fakeTopicsClient{}, 12345))
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.create", map[string]any{"name": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE for positive chat ID, got %+v", resp)
	}
}

func TestTopicsCreate_ClientUnavailable(t *testing.T) {
	deps := TopicsDeps{
		Client: func() (TopicsClient, error) {
			return nil, errors.New("plugin not started")
		},
		ActiveChatID: func() int64 { return -100 },
	}
	h := topicsCreate(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.create", map[string]any{"name": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE when client factory errs, got %+v", resp)
	}
}

func TestTopicsCreate_APIFailureSurfacesAsDependencyFailed(t *testing.T) {
	client := &fakeTopicsClient{
		createFn: func(_ context.Context, _ int64, _ string, _ int64) (*telegram.ForumTopic, error) {
			return nil, errors.New("403: CHAT_ADMIN_REQUIRED")
		},
	}
	h := topicsCreate(topicsDepsFor(client, -100))
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.create", map[string]any{"name": "x"}))
	if resp.OK || resp.Error.Code != protocol.ErrDependencyFailed {
		t.Errorf("expected DEPENDENCY_FAILED on Bot API failure, got %+v", resp)
	}
}

func TestTopicsMethods_NilFactoryReturnsNil(t *testing.T) {
	if got := TopicsMethods(TopicsDeps{Client: nil}); got != nil {
		t.Errorf("TopicsMethods with nil deps = %v, want nil", got)
	}
}

func TestTopicsMethods_RegistersPerDependency(t *testing.T) {
	// Only Client wired → create only.
	m := TopicsMethods(TopicsDeps{Client: func() (TopicsClient, error) { return &fakeTopicsClient{}, nil }})
	if _, ok := m["miniapp.topics.create"]; !ok {
		t.Error("create not registered when Client wired")
	}
	if _, ok := m["miniapp.topics.list"]; ok {
		t.Error("list registered without KnowledgeTopics")
	}
	// Only KnowledgeTopics wired → list only (gateway with no Telegram plugin).
	m = TopicsMethods(TopicsDeps{KnowledgeTopics: func() []KnowledgeTopic { return nil }})
	if _, ok := m["miniapp.topics.list"]; !ok {
		t.Error("list not registered when KnowledgeTopics wired")
	}
	if _, ok := m["miniapp.topics.create"]; ok {
		t.Error("create registered without Client")
	}
}

func TestTopicsList_SortsGeneralFirstThenByKey(t *testing.T) {
	deps := TopicsDeps{KnowledgeTopics: func() []KnowledgeTopic {
		// Intentionally unsorted; General ("0") is last in the input.
		return []KnowledgeTopic{
			{Key: "잡담", ThreadID: "57"},
			{Key: "코딩", ThreadID: "42"},
			{Key: "업무", ThreadID: "0"},
		}
	}}
	h := topicsList(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.list", map[string]any{}))
	var got struct {
		Topics []KnowledgeTopic `json:"topics"`
	}
	decode(t, resp, &got)
	if len(got.Topics) != 3 {
		t.Fatalf("got %d topics, want 3: %+v", len(got.Topics), got.Topics)
	}
	if got.Topics[0].ThreadID != "0" || got.Topics[0].Key != "업무" {
		t.Errorf("General not first: %+v", got.Topics)
	}
	// Remaining sorted by key: "잡담" < "코딩" (Hangul codepoint order).
	if got.Topics[1].Key != "잡담" || got.Topics[2].Key != "코딩" {
		t.Errorf("non-General topics not key-sorted: %+v", got.Topics)
	}
}

func TestTopicsList_RequiresAuth(t *testing.T) {
	h := topicsList(TopicsDeps{KnowledgeTopics: func() []KnowledgeTopic { return nil }})
	resp := h(context.Background(), reqWith(t, "miniapp.topics.list", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("auth not enforced: %+v", resp)
	}
}

func TestTopicsList_EmptyWhenUnconfigured(t *testing.T) {
	h := topicsList(TopicsDeps{KnowledgeTopics: func() []KnowledgeTopic { return nil }})
	resp := h(authedCtx(), reqWith(t, "miniapp.topics.list", map[string]any{}))
	var got struct {
		Topics []KnowledgeTopic `json:"topics"`
	}
	decode(t, resp, &got)
	if len(got.Topics) != 0 {
		t.Errorf("expected empty topics, got %+v", got.Topics)
	}
}
