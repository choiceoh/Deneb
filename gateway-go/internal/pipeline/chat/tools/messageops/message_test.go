package messageops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func TestToolMessageSendErrorsWithoutConnectedChannel(t *testing.T) {
	tool := ToolMessage()

	_, err := tool(context.Background(), []byte(`{"action":"send","message":"hello"}`))
	if err == nil {
		t.Fatal("expected error when reply function is unavailable")
	}
	if !strings.Contains(err.Error(), "in-loop send is not wired") {
		t.Fatalf("got %q, want channel-not-connected error", err)
	}
}

func TestToolMessageSendErrorsWithoutDeliveryTarget(t *testing.T) {
	tool := ToolMessage()
	ctx := toolport.WithReplyFunc(context.Background(), func(ctx context.Context, delivery *toolport.DeliveryContext, text string) error {
		t.Fatalf("replyFn should not be called without a delivery target")
		return nil
	})

	_, err := tool(ctx, []byte(`{"action":"send","message":"hello"}`))
	if err == nil {
		t.Fatal("expected error when delivery target is missing")
	}
	if !strings.Contains(err.Error(), "in-loop send has no delivery target") {
		t.Fatalf("got %q, want missing-target error", err)
	}
}

func TestToolMessageSendAutoDeliveryIgnoresMissingReplyFunc(t *testing.T) {
	tool := ToolMessage()
	ctx := toolport.WithAutoDelivery(context.Background())

	out, err := tool(ctx, []byte(`{"action":"send","message":"hello"}`))
	if err != nil {
		t.Fatalf("auto-delivery run must not error on unwired in-loop send: %v", err)
	}
	if !strings.Contains(out, "scheduled run") {
		t.Fatalf("got %q, want benign scheduled-run skip notice", out)
	}
}

func TestToolMessageSendAutoDeliveryIgnoresMissingDeliveryTarget(t *testing.T) {
	tool := ToolMessage()
	ctx := toolport.WithAutoDelivery(context.Background())
	ctx = toolport.WithReplyFunc(ctx, func(ctx context.Context, delivery *toolport.DeliveryContext, text string) error {
		t.Fatalf("replyFn should not be called without a delivery target")
		return nil
	})

	out, err := tool(ctx, []byte(`{"action":"send","message":"hello"}`))
	if err != nil {
		t.Fatalf("auto-delivery run must not error on missing in-loop target: %v", err)
	}
	if !strings.Contains(out, "scheduled run") {
		t.Fatalf("got %q, want benign scheduled-run skip notice", out)
	}
}

func TestToolMessageSendPropagatesDeliveryFailure(t *testing.T) {
	tool := ToolMessage()
	wantErr := errors.New("telegram client not connected")
	ctx := toolport.WithDeliveryContext(context.Background(), &toolport.DeliveryContext{
		Channel: "telegram",
		To:      "telegram:123",
	})
	ctx = toolport.WithReplyFunc(ctx, func(ctx context.Context, delivery *toolport.DeliveryContext, text string) error {
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

func TestToolMessageSendReturnsSuccessWithCurrentDelivery(t *testing.T) {
	tool := ToolMessage()
	var gotDelivery *toolport.DeliveryContext
	var gotText string

	ctx := toolport.WithDeliveryContext(context.Background(), &toolport.DeliveryContext{
		Channel: "telegram",
		To:      "telegram:123",
	})
	ctx = toolport.WithReplyFunc(ctx, func(ctx context.Context, delivery *toolport.DeliveryContext, text string) error {
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
