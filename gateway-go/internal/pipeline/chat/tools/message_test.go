package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

func TestToolMessageSendRequiresConnectedChannel(t *testing.T) {
	tool := ToolMessage()

	_, err := tool(context.Background(), []byte(`{"action":"send","message":"hello"}`))
	if err == nil {
		t.Fatal("expected error when reply function is unavailable")
	}
	if !strings.Contains(err.Error(), "channel not connected") {
		t.Fatalf("got %q, want channel-not-connected error", err)
	}
}

func TestToolMessageSendRequiresDeliveryTarget(t *testing.T) {
	tool := ToolMessage()
	ctx := toolctx.WithReplyFunc(context.Background(), func(ctx context.Context, delivery *toolctx.DeliveryContext, text string) error {
		t.Fatalf("replyFn should not be called without a delivery target")
		return nil
	})

	_, err := tool(ctx, []byte(`{"action":"send","message":"hello"}`))
	if err == nil {
		t.Fatal("expected error when delivery target is missing")
	}
	if !strings.Contains(err.Error(), "no active delivery target") {
		t.Fatalf("got %q, want missing-target error", err)
	}
}

func TestToolMessageSendPropagatesDeliveryFailure(t *testing.T) {
	tool := ToolMessage()
	wantErr := errors.New("telegram client not connected")
	ctx := toolctx.WithDeliveryContext(context.Background(), &toolctx.DeliveryContext{
		Channel: "telegram",
		To:      "telegram:123",
	})
	ctx = toolctx.WithReplyFunc(ctx, func(ctx context.Context, delivery *toolctx.DeliveryContext, text string) error {
		return wantErr
	})

	_, err := tool(ctx, []byte(`{"action":"send","message":"hello"}`))
	if err == nil {
		t.Fatal("expected delivery error")
	}
	if !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("got %q, want not-confirmed error", err)
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("got %q, want wrapped transport error", err)
	}
}

func TestToolMessageSendSuccessUsesCurrentDelivery(t *testing.T) {
	tool := ToolMessage()
	var gotDelivery *toolctx.DeliveryContext
	var gotText string

	ctx := toolctx.WithDeliveryContext(context.Background(), &toolctx.DeliveryContext{
		Channel: "telegram",
		To:      "telegram:123",
	})
	ctx = toolctx.WithReplyFunc(ctx, func(ctx context.Context, delivery *toolctx.DeliveryContext, text string) error {
		gotDelivery = delivery
		gotText = text
		return nil
	})

	out, err := tool(ctx, []byte(`{"action":"send","message":"hello"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "Message sent successfully." {
		t.Fatalf("got %q, want success message", out)
	}
	if gotDelivery == nil || gotDelivery.Channel != "telegram" || gotDelivery.To != "telegram:123" {
		t.Fatalf("unexpected delivery: %#v", gotDelivery)
	}
	if gotText != "hello" {
		t.Fatalf("got text %q, want hello", gotText)
	}
}
