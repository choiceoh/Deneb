package streaming

import (
	"context"
	"testing"
)

func TestDispatch_EmptyTargets(t *testing.T) {
	results := Dispatch(context.Background(), nil, nil, "Hello!", nil)
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestDispatch_NilPlugin(t *testing.T) {
	results := Dispatch(context.Background(), nil,
		[]DeliveryTarget{{Channel: "telegram", To: "user1"}},
		"Hello!", nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Delivered {
		t.Error("expected not delivered with nil plugin")
	}
	if results[0].Error == nil {
		t.Error("expected error for nil plugin")
	}
}

func TestDispatch_NonTelegramChannel(t *testing.T) {
	results := Dispatch(context.Background(), nil,
		[]DeliveryTarget{{Channel: "slack", To: "user1"}},
		"Hello!", nil)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Delivered {
		t.Error("expected not delivered for non-telegram channel")
	}
}
