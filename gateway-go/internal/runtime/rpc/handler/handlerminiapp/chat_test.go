package handlerminiapp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeChatSender struct {
	sendFn func(ctx context.Context, sessionKey, message, model string, opts *chat.SyncOptions) (*chat.SyncResult, error)
}

func (f *fakeChatSender) SendSync(
	ctx context.Context, sessionKey, message, model string, opts *chat.SyncOptions,
) (*chat.SyncResult, error) {
	if f.sendFn == nil {
		return nil, errors.New("SendSync not stubbed")
	}
	return f.sendFn(ctx, sessionKey, message, model, opts)
}

func chatDepsFor(sender ChatSender) ChatDeps {
	return ChatDeps{Sender: func() (ChatSender, error) { return sender, nil }}
}

func TestChatSend_HappyPath_UsesDerivedSessionKey(t *testing.T) {
	var seenKey, seenMsg, seenModel string
	sender := &fakeChatSender{
		sendFn: func(_ context.Context, key, msg, model string, _ *chat.SyncOptions) (*chat.SyncResult, error) {
			seenKey, seenMsg, seenModel = key, msg, model
			return &chat.SyncResult{
				Text:         "안녕하세요 — Deneb 입니다.",
				Model:        "glm-5.1",
				InputTokens:  12,
				OutputTokens: 8,
				StopReason:   "stop",
			}, nil
		},
	}
	h := chatSend(chatDepsFor(sender))
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{
		"message": "안녕",
	}))

	var got map[string]any
	decode(t, resp, &got)
	if seenMsg != "안녕" {
		t.Errorf("message = %q", seenMsg)
	}
	// Derived from sampleInitData().User.ID = 42.
	if seenKey != "miniapp:42" {
		t.Errorf("sessionKey = %q, want miniapp:42", seenKey)
	}
	if seenModel != "" {
		t.Errorf("model = %q, want empty (server default)", seenModel)
	}
	if got["response"] != "안녕하세요 — Deneb 입니다." {
		t.Errorf("response = %v", got["response"])
	}
	if got["model"] != "glm-5.1" {
		t.Errorf("model = %v", got["model"])
	}
	if int(got["inputTokens"].(float64)) != 12 || int(got["outputTokens"].(float64)) != 8 {
		t.Errorf("tokens missing: %+v", got)
	}
	if _, ok := got["durationMs"].(float64); !ok {
		t.Errorf("durationMs missing/non-numeric: %v", got["durationMs"])
	}
}

func TestChatSend_ExplicitSessionKey(t *testing.T) {
	var seenKey string
	sender := &fakeChatSender{
		sendFn: func(_ context.Context, key, _, _ string, _ *chat.SyncOptions) (*chat.SyncResult, error) {
			seenKey = key
			return &chat.SyncResult{Text: "ok"}, nil
		},
	}
	h := chatSend(chatDepsFor(sender))
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{
		"message":    "x",
		"sessionKey": "miniapp:custom-session",
	}))
	if !resp.OK {
		t.Fatalf("response error: %+v", resp.Error)
	}
	if seenKey != "miniapp:custom-session" {
		t.Errorf("sessionKey = %q, want miniapp:custom-session", seenKey)
	}
}

func TestChatSend_ExplicitModel(t *testing.T) {
	var seenModel string
	sender := &fakeChatSender{
		sendFn: func(_ context.Context, _, _, model string, _ *chat.SyncOptions) (*chat.SyncResult, error) {
			seenModel = model
			return &chat.SyncResult{Text: "ok"}, nil
		},
	}
	h := chatSend(chatDepsFor(sender))
	h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{
		"message": "x", "model": "anthropic/claude-opus",
	}))
	if seenModel != "anthropic/claude-opus" {
		t.Errorf("model = %q", seenModel)
	}
}

func TestChatSend_FallsBackToAllTextWhenTextEmpty(t *testing.T) {
	sender := &fakeChatSender{
		sendFn: func(_ context.Context, _, _, _ string, _ *chat.SyncOptions) (*chat.SyncResult, error) {
			return &chat.SyncResult{Text: "", AllText: "도구 호출 결과만 있었음"}, nil
		},
	}
	h := chatSend(chatDepsFor(sender))
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{
		"message": "x",
	}))
	var got map[string]any
	decode(t, resp, &got)
	if got["response"] != "도구 호출 결과만 있었음" {
		t.Errorf("response = %v, want AllText fallback", got["response"])
	}
}

func TestChatSend_MissingMessage(t *testing.T) {
	h := chatSend(chatDepsFor(&fakeChatSender{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want MISSING_PARAM", resp.Error.Code)
	}
}

func TestChatSend_BlankMessage(t *testing.T) {
	h := chatSend(chatDepsFor(&fakeChatSender{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{"message": "   "}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want MISSING_PARAM", resp.Error.Code)
	}
}

func TestChatSend_TooLongMessageRejected(t *testing.T) {
	// 8001 runes — one over the cap.
	long := strings.Repeat("가", maxChatMessageRunes+1)
	h := chatSend(chatDepsFor(&fakeChatSender{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{"message": long}))
	if resp.OK {
		t.Fatalf("expected error for oversize message")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestChatSend_RequiresAuth(t *testing.T) {
	h := chatSend(chatDepsFor(&fakeChatSender{}))
	resp := h(context.Background(), reqWith(t, "miniapp.chat.send", map[string]any{"message": "x"}))
	if resp.OK {
		t.Fatalf("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestChatSend_NoUserNoExplicitKey(t *testing.T) {
	// InitData with no User → can't derive default session key.
	ctxNoUser := telegram.WithInitDataContext(context.Background(), &telegram.InitData{})
	h := chatSend(chatDepsFor(&fakeChatSender{
		sendFn: func(_ context.Context, _, _, _ string, _ *chat.SyncOptions) (*chat.SyncResult, error) {
			t.Fatalf("SendSync should not be called when session key can't be derived")
			return nil, nil
		},
	}))
	resp := h(ctxNoUser, reqWith(t, "miniapp.chat.send", map[string]any{"message": "x"}))
	if resp.OK {
		t.Fatalf("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestChatSend_SenderError(t *testing.T) {
	sender := &fakeChatSender{
		sendFn: func(_ context.Context, _, _, _ string, _ *chat.SyncOptions) (*chat.SyncResult, error) {
			return nil, errors.New("LLM timed out")
		},
	}
	h := chatSend(chatDepsFor(sender))
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{"message": "x"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestChatSend_NilResult(t *testing.T) {
	sender := &fakeChatSender{
		sendFn: func(_ context.Context, _, _, _ string, _ *chat.SyncOptions) (*chat.SyncResult, error) {
			return nil, nil
		},
	}
	h := chatSend(chatDepsFor(sender))
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{"message": "x"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestChatSend_FactoryError(t *testing.T) {
	deps := ChatDeps{
		Sender: func() (ChatSender, error) {
			return nil, errors.New("chat init not done")
		},
	}
	h := chatSend(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.chat.send", map[string]any{"message": "x"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestChatMethods_NilSenderReturnsNil(t *testing.T) {
	if got := ChatMethods(ChatDeps{Sender: nil}); got != nil {
		t.Errorf("ChatMethods(nil) = %v, want nil", got)
	}
}
