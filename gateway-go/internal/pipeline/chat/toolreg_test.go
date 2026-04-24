package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestToolRegistry_Execute(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register("echo", func(_ context.Context, input json.RawMessage) (string, error) {
		return string(input), nil
	})

	result := testutil.Must(reg.Execute(context.Background(), "echo", json.RawMessage(`"hello"`)))
	if result != `"hello"` {
		t.Errorf("result = %q, want %q", result, `"hello"`)
	}
}

func TestToolRegistry_UnknownTool(t *testing.T) {
	reg := NewToolRegistry()
	_, err := reg.Execute(context.Background(), "missing", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

// TestToolRegistry_AutoSpillover_OverThreshold verifies that a tool returning
// more than agent.DefaultMaxOutput chars is automatically spilled to disk and
// the value returned to the caller is the trim marker (head+tail) — not the
// raw content. The spill must be loadable by the same session via the store.
func TestToolRegistry_AutoSpillover_OverThreshold(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())

	reg := NewToolRegistry()
	reg.SetSpilloverStore(store)

	big := strings.Repeat("Z", agent.DefaultMaxOutput+4096)
	reg.Register("big", func(_ context.Context, _ json.RawMessage) (string, error) {
		return big, nil
	})

	ctx := toolctx.WithSessionKey(context.Background(), "sess-auto")
	result := testutil.Must(reg.Execute(ctx, "big", json.RawMessage(`{}`)))

	if len(result) >= len(big) {
		t.Fatalf("result should be trimmed, got %d chars (input was %d)", len(result), len(big))
	}
	if !strings.Contains(result, "read_spillover") {
		t.Errorf("trim marker should reference read_spillover, got: %q", firstN(result, 200))
	}
	// Extract the spill ID from the marker and verify it is retrievable.
	id := extractSpillID(result)
	if id == "" {
		t.Fatalf("could not locate spill ID in result: %q", firstN(result, 300))
	}
	loaded := testutil.Must(store.Load(id, "sess-auto"))
	if loaded != big {
		t.Errorf("loaded spill content mismatch: got %d chars, want %d", len(loaded), len(big))
	}
}

// TestToolRegistry_AutoSpillover_BelowThreshold verifies the pass-through path:
// outputs within DefaultMaxOutput are returned verbatim and no spill file is
// created.
func TestToolRegistry_AutoSpillover_BelowThreshold(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())

	reg := NewToolRegistry()
	reg.SetSpilloverStore(store)

	small := strings.Repeat("s", 256)
	reg.Register("small", func(_ context.Context, _ json.RawMessage) (string, error) {
		return small, nil
	})

	ctx := toolctx.WithSessionKey(context.Background(), "sess-small")
	result := testutil.Must(reg.Execute(ctx, "small", json.RawMessage(`{}`)))

	if result != small {
		t.Errorf("small output should pass through unchanged, got %d chars", len(result))
	}
	if strings.Contains(result, "read_spillover") {
		t.Errorf("no spillover reference should appear for small output")
	}
}

// extractSpillID finds the first sp_XXXXXXXX identifier in s. Mirrors the
// format written by SpilloverStore.Store (sp_ + 8 hex chars).
func extractSpillID(s string) string {
	idx := strings.Index(s, "sp_")
	if idx < 0 {
		return ""
	}
	end := idx + 3
	for end < len(s) && isHexByte(s[end]) {
		end++
	}
	if end-idx < 4 { // "sp_" + at least one hex
		return ""
	}
	return s[idx:end]
}

func isHexByte(b byte) bool {
	switch {
	case b >= '0' && b <= '9':
		return true
	case b >= 'a' && b <= 'f':
		return true
	case b >= 'A' && b <= 'F':
		return true
	}
	return false
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
