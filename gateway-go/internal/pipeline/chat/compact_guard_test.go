package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// TestHashMessages_Stable verifies that hashing the same messages slice
// twice produces the same digest — this is the invariant the idempotent-
// compaction skip relies on.
func TestHashMessages_Stable(t *testing.T) {
	msgs := []llm.Message{
		llm.NewTextMessage("user", "hello"),
		llm.NewTextMessage("assistant", "hi there"),
	}
	h1 := hashMessages(msgs)
	h2 := hashMessages(msgs)
	if h1 != h2 {
		t.Fatalf("hash mismatch for identical inputs: %q != %q", h1, h2)
	}
}

// TestHashMessages_RoleSeparation verifies that two messages whose content
// is byte-identical but whose role differs hash to different values.
// Without the role separator, [{user, "X"}, {assistant, ""}] would collide
// with [{user, ""}, {assistant, "X"}].
func TestHashMessages_RoleSeparation(t *testing.T) {
	a := []llm.Message{
		llm.NewTextMessage("user", "X"),
		llm.NewTextMessage("assistant", ""),
	}
	b := []llm.Message{
		llm.NewTextMessage("user", ""),
		llm.NewTextMessage("assistant", "X"),
	}
	if hashMessages(a) == hashMessages(b) {
		t.Fatal("role-shifted content should produce different hashes")
	}
}

// TestHashMessages_ContentChange verifies that changing one byte of content
// changes the hash — this is what guarantees we DON'T skip compaction when
// the cheap-first shrink pipeline actually changed something.
func TestHashMessages_ContentChange(t *testing.T) {
	a := []llm.Message{llm.NewTextMessage("user", "hello world")}
	b := []llm.Message{llm.NewTextMessage("user", "hello World")}
	if hashMessages(a) == hashMessages(b) {
		t.Fatal("content difference should produce different hashes")
	}
}

// TestProtectedZoneExceedsBudget_SmallSessionUnderBudget verifies the
// common case: a session whose head+tail fits comfortably within budget
// returns false (compaction is viable).
func TestProtectedZoneExceedsBudget_SmallSessionUnderBudget(t *testing.T) {
	msgs := []llm.Message{
		llm.NewTextMessage("user", "hi"),
		llm.NewTextMessage("assistant", "hello"),
		llm.NewTextMessage("user", "how are you"),
		llm.NewTextMessage("assistant", "fine"),
	}
	if protectedZoneExceedsBudget(msgs, 1000) {
		t.Fatal("small session should not be marked stuck")
	}
}

// TestProtectedZoneExceedsBudget_HeadTailOverflow verifies the thrashing
// detection: when the head (2 messages) + tail (6 messages) alone already
// exceed budget, the guard returns true so the caller can bail out instead
// of retrying.
func TestProtectedZoneExceedsBudget_HeadTailOverflow(t *testing.T) {
	// Build 15 messages. Head=msgs[0:2], tail=msgs[9:15]. Pump each protected
	// message up to 4KB (~2048 tokens) so head+tail well exceeds the 2000-
	// token budget.
	huge := strings.Repeat("x", 4096)
	msgs := make([]llm.Message, 15)
	for i := range msgs {
		msgs[i] = llm.NewTextMessage("user", huge)
	}

	if !protectedZoneExceedsBudget(msgs, 2000) {
		t.Fatalf("head+tail of %d×4KB messages should exceed budget 2000", len(msgs))
	}
}

// TestProtectedZoneExceedsBudget_SmallerThanProtectedZone verifies the
// all-protected path: when len(messages) <= headCount+tailCount, all
// messages are protected. Compaction is viable only if the total fits
// budget; otherwise the session is stuck.
func TestProtectedZoneExceedsBudget_SmallerThanProtectedZone(t *testing.T) {
	// 5 messages, each 4KB (~2048 tokens). Total ~10240 tokens.
	huge := strings.Repeat("x", 4096)
	msgs := make([]llm.Message, 5)
	for i := range msgs {
		msgs[i] = llm.NewTextMessage("user", huge)
	}

	// Under budget: not stuck.
	if protectedZoneExceedsBudget(msgs, 100_000) {
		t.Fatal("5×4KB messages should fit in 100K budget")
	}
	// Over budget: stuck.
	if !protectedZoneExceedsBudget(msgs, 5000) {
		t.Fatal("5×4KB messages should overflow 5K budget")
	}
}

// TestProtectedZoneExceedsBudget_NonPositiveBudget verifies that a
// non-positive budget disables the guard (returns false) — used by boot
// sessions and subagents that don't configure a budget.
func TestProtectedZoneExceedsBudget_NonPositiveBudget(t *testing.T) {
	msgs := make([]llm.Message, 10)
	for i := range msgs {
		msgs[i] = llm.NewTextMessage("user", strings.Repeat("x", 10_000))
	}
	if protectedZoneExceedsBudget(msgs, 0) {
		t.Fatal("zero budget should disable the guard")
	}
	if protectedZoneExceedsBudget(msgs, -1) {
		t.Fatal("negative budget should disable the guard")
	}
}

// TestFallbackForStopReason_CompressionStuck verifies the Korean user-
// visible message is returned for the new stop reason.
func TestFallbackForStopReason_CompressionStuck(t *testing.T) {
	got := fallbackForStopReason(stopReasonCompressionStuck)
	if got == "" {
		t.Fatal("fallbackForStopReason returned empty for compression_stuck")
	}
	if !strings.Contains(got, "/reset") {
		t.Errorf("message should recommend /reset, got: %q", got)
	}
	// Must be Korean.
	hasHangul := false
	for _, r := range got {
		if r >= 0xAC00 && r <= 0xD7A3 {
			hasHangul = true
			break
		}
	}
	if !hasHangul {
		t.Errorf("message should be Korean, got: %q", got)
	}
}
