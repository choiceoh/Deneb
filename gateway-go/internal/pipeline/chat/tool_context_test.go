package chat

import (
	"context"
	"testing"
)

func TestDeliveryContext(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		d := &DeliveryContext{
			Channel:   "telegram",
			To:        "user123",
			AccountID: "acc-1",
			ThreadID:  "th-1",
			MessageID: "msg-1",
		}
		ctx := WithDeliveryContext(context.Background(), d)
		got := DeliveryFromContext(ctx)
		if got == nil {
			t.Fatal("expected non-nil DeliveryContext")
		}
		if got.Channel != "telegram" {
			t.Errorf("Channel = %q, want %q", got.Channel, "telegram")
		}
		if got.To != "user123" {
			t.Errorf("To = %q, want %q", got.To, "user123")
		}
		if got.AccountID != "acc-1" {
			t.Errorf("AccountID = %q, want %q", got.AccountID, "acc-1")
		}
	})

	t.Run("missing returns nil", func(t *testing.T) {
		got := DeliveryFromContext(context.Background())
		if got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})
}

func TestReplyFunc(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		called := false
		fn := ReplyFunc(func(ctx context.Context, d *DeliveryContext, text string) error {
			called = true
			return nil
		})
		ctx := WithReplyFunc(context.Background(), fn)
		got := ReplyFuncFromContext(ctx)
		if got == nil {
			t.Fatal("expected non-nil ReplyFunc")
		}
		_ = got(context.Background(), nil, "test")
		if !called {
			t.Error("ReplyFunc was not called")
		}
	})

	t.Run("missing returns nil", func(t *testing.T) {
		got := ReplyFuncFromContext(context.Background())
		if got != nil {
			t.Error("expected nil ReplyFunc")
		}
	})
}

func TestSessionKey(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		ctx := WithSessionKey(context.Background(), "session-abc")
		got := SessionKeyFromContext(ctx)
		if got != "session-abc" {
			t.Errorf("session key = %q, want %q", got, "session-abc")
		}
	})

	t.Run("missing returns empty string", func(t *testing.T) {
		got := SessionKeyFromContext(context.Background())
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})
}
