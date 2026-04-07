package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestToolMessageSend(t *testing.T) {
	fn := toolMessage()

	t.Run("send requires message", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"action": "send"})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for missing message")
		}
	})

	t.Run("no reply func returns info", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{"action": "send", "message": "hello"})
		result, err := fn(context.Background(), input)
		testutil.NoError(t, err)
		if result != "Message tool: no reply function available (channel not connected)." {
			t.Errorf("unexpected result: %s", result)
		}
	})

	t.Run("send with reply func", func(t *testing.T) {
		var sentText string
		var sentDelivery *DeliveryContext
		replyFn := ReplyFunc(func(ctx context.Context, d *DeliveryContext, text string) error {
			sentText = text
			sentDelivery = d
			return nil
		})
		delivery := &DeliveryContext{Channel: "telegram", To: "user1"}

		ctx := WithReplyFunc(context.Background(), replyFn)
		ctx = WithDeliveryContext(ctx, delivery)

		input, _ := json.Marshal(map[string]any{"action": "send", "message": "hello world"})
		result, err := fn(ctx, input)
		testutil.NoError(t, err)
		if result != "Message sent successfully." {
			t.Errorf("result = %q, want success message", result)
		}
		if sentText != "hello world" {
			t.Errorf("sentText = %q, want %q", sentText, "hello world")
		}
		if sentDelivery.Channel != "telegram" {
			t.Errorf("channel = %q, want %q", sentDelivery.Channel, "telegram")
		}
	})

	t.Run("send overrides delivery context", func(t *testing.T) {
		var sentDelivery *DeliveryContext
		replyFn := ReplyFunc(func(ctx context.Context, d *DeliveryContext, text string) error {
			sentDelivery = d
			return nil
		})
		delivery := &DeliveryContext{Channel: "telegram", To: "user1"}

		ctx := WithReplyFunc(context.Background(), replyFn)
		ctx = WithDeliveryContext(ctx, delivery)

		input, _ := json.Marshal(map[string]any{
			"action":  "send",
			"message": "hello",
			"to":      "user2",
			"channel": "telegram",
		})
		_, err := fn(ctx, input)
		testutil.NoError(t, err)
		if sentDelivery.To != "user2" {
			t.Errorf("to = %q, want %q", sentDelivery.To, "user2")
		}
		if sentDelivery.Channel != "telegram" {
			t.Errorf("channel = %q, want %q", sentDelivery.Channel, "telegram")
		}
	})
}

func TestToolMessageReact(t *testing.T) {
	fn := toolMessage()

	t.Run("react requires emoji", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"action":    "react",
			"messageId": "msg-1",
		})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for missing emoji")
		}
	})

	t.Run("react requires messageId", func(t *testing.T) {
		input, _ := json.Marshal(map[string]any{
			"action": "react",
			"emoji":  "👍",
		})
		_, err := fn(context.Background(), input)
		if err == nil {
			t.Fatal("expected error for missing messageId")
		}
	})

	t.Run("react sends payload", func(t *testing.T) {
		var sentText string
		replyFn := ReplyFunc(func(ctx context.Context, d *DeliveryContext, text string) error {
			sentText = text
			return nil
		})

		ctx := WithReplyFunc(context.Background(), replyFn)
		ctx = WithDeliveryContext(ctx, &DeliveryContext{Channel: "telegram"})

		input, _ := json.Marshal(map[string]any{
			"action":    "react",
			"emoji":     "👍",
			"messageId": "msg-123",
		})
		result, err := fn(ctx, input)
		testutil.NoError(t, err)
		if sentText != "__react:msg-123:👍" {
			t.Errorf("sent payload = %q, want react marker", sentText)
		}
		if result != "Reaction 👍 sent to message msg-123." {
			t.Errorf("result = %q", result)
		}
	})
}

func TestToolMessageUnknownAction(t *testing.T) {
	fn := toolMessage()
	input, _ := json.Marshal(map[string]any{"action": "delete"})
	result, err := fn(context.Background(), input)
	testutil.NoError(t, err)
	if result != `Unknown message action: "delete". Supported: send, reply, react.` {
		t.Errorf("result = %q", result)
	}
}

func TestToolMessageDefaultAction(t *testing.T) {
	fn := toolMessage()
	// Empty action should default to "send".
	input, _ := json.Marshal(map[string]any{"message": "hello"})
	// No reply func → should return info message, proving default action is "send".
	result, err := fn(context.Background(), input)
	testutil.NoError(t, err)
	if result != "Message tool: no reply function available (channel not connected)." {
		t.Errorf("unexpected result: %s", result)
	}
}
