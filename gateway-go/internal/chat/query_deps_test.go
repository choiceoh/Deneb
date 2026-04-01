package chat

import (
	"context"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

func TestProductionQueryDeps(t *testing.T) {
	deps := ProductionQueryDeps()

	t.Run("has all fields", func(t *testing.T) {
		if deps.CompleteModel == nil {
			t.Error("CompleteModel is nil")
		}
		if deps.StreamModel == nil {
			t.Error("StreamModel is nil")
		}
		if deps.Microcompact == nil {
			t.Error("Microcompact is nil")
		}
		if deps.EvaluateCompaction == nil {
			t.Error("EvaluateCompaction is nil")
		}
		if deps.GenerateUUID == nil {
			t.Error("GenerateUUID is nil")
		}
	})

	t.Run("UUID is unique", func(t *testing.T) {
		id1 := deps.GenerateUUID()
		time.Sleep(2 * time.Millisecond)
		id2 := deps.GenerateUUID()
		if id1 == id2 {
			t.Errorf("UUIDs should be unique: %q == %q", id1, id2)
		}
	})
}

func TestQueryDeps_MockOverride(t *testing.T) {
	// Demonstrate the DI pattern: override specific deps for testing.
	deps := ProductionQueryDeps()

	callCount := 0
	deps.CompleteModel = func(_ context.Context, _ *llm.Client, _ llm.ChatRequest) (string, error) {
		callCount++
		return "mocked response", nil
	}

	deps.GenerateUUID = func() string {
		return "test-uuid-123"
	}

	// Use the mocked deps.
	result, _ := deps.CompleteModel(context.Background(), nil, llm.ChatRequest{})
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1", callCount)
	}
	if result != "mocked response" {
		t.Errorf("result = %q", result)
	}

	uuid := deps.GenerateUUID()
	if uuid != "test-uuid-123" {
		t.Errorf("uuid = %q, want test-uuid-123", uuid)
	}
}
