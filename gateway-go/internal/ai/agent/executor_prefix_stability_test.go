package agent

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// firstDivergentMessage returns the index of the first message whose bytes
// differ between two consecutive turn snapshots, or -1 when prev is a
// byte-identical prefix of cur (append-only — the property a content-prefix
// cache like kimi needs to reuse the whole prior turn instead of re-billing it).
func firstDivergentMessage(prev, cur [][]byte) int {
	for i := range prev {
		if i >= len(cur) || !bytes.Equal(prev[i], cur[i]) {
			return i
		}
	}
	return -1
}

// runToolLoop drives a fresh run: n tool-call turns (each returning a large
// tool result) then a final text turn, and returns the streamer holding the
// per-turn request-message snapshots.
func runToolLoop(t *testing.T, cfg AgentConfig, toolResult string, nToolTurns int) *fakeLLMStreamer {
	t.Helper()
	tools := newFakeToolExecutor()
	tools.outputs["read"] = toolResult

	var turns [][]llm.StreamEvent
	for i := 0; i < nToolTurns; i++ {
		turns = append(turns, buildToolUseTurnEventsWithNames([]toolUseSpec{
			{id: fmt.Sprintf("toolu_%d", i), name: "read", inputJSON: `{"path":"a.go"}`},
		}, 100, 30))
	}
	turns = append(turns, buildTextTurnEvents("done", 100, 20))

	streamer := &fakeLLMStreamer{turns: turns}
	messages := []llm.Message{llm.NewTextMessage("user", "read a.go repeatedly")}
	testutil.Must(RunAgent(context.Background(), cfg, messages, streamer, tools, StreamHooks{}, nil, nil))
	return streamer
}

// TestRunAgent_ContentPrefixConfig_WithinRunIsAppendOnly is the decisive test
// for the kimi within-run cache question: does the gateway send byte-identical
// prefixes turn-to-turn within one run (so a stall must be provider-side write
// latency), or does it mutate already-sent history (a gateway bug)?
//
// With the content-prefix-cache configuration (DisablePriorToolResultCompaction,
// set for kimi in run_exec.go), every request's message list must be a strict
// byte-prefix extension of the previous turn's. A large (>CompactedMaxOutput)
// tool result on every turn is exactly what would be rewritten if the gate were
// off — so this both proves append-only AND guards the gate against regressing.
func TestRunAgent_ContentPrefixConfig_WithinRunIsAppendOnly(t *testing.T) {
	big := strings.Repeat("abcd ", 2000) // 10K chars > CompactedMaxOutput (4K), < DefaultMaxOutput (24K)
	cfg := AgentConfig{
		MaxTurns:                         10,
		Timeout:                          10 * time.Second,
		MaxTokens:                        4096,
		DisablePriorToolResultCompaction: true, // the kimi/content-prefix setting
	}
	streamer := runToolLoop(t, cfg, big, 3)

	if len(streamer.recordedMsgs) < 4 {
		t.Fatalf("expected >=4 recorded requests (3 tool turns + 1 text), got %d", len(streamer.recordedMsgs))
	}
	for turn := 1; turn < len(streamer.recordedMsgs); turn++ {
		prev, cur := streamer.recordedMsgs[turn-1], streamer.recordedMsgs[turn]
		if len(cur) < len(prev) {
			t.Fatalf("turn %d shrank the message list (%d < %d) — not append-only", turn, len(cur), len(prev))
		}
		if d := firstDivergentMessage(prev, cur); d >= 0 {
			t.Errorf("turn %d message[%d] diverges from turn %d — within-run prefix MUTATED, not append-only "+
				"(content-prefix cache would re-bill from here). This is a gateway bug, not provider latency.",
				turn, d, turn-1)
		}
	}
}

// TestRunAgent_CompactionEnabled_MutatesPriorToolResultWithinRun is the
// contrast: with the gate OFF, CompactPriorToolResults shrinks a prior turn's
// large tool_result in place, so a later request is NOT a byte-prefix extension.
// This proves (a) the snapshot harness actually detects mutation, and (b) the
// DisablePriorToolResultCompaction gate is precisely what makes the kimi path
// append-only above.
func TestRunAgent_CompactionEnabled_MutatesPriorToolResultWithinRun(t *testing.T) {
	big := strings.Repeat("abcd ", 2000) // > CompactedMaxOutput so compaction rewrites it
	cfg := AgentConfig{
		MaxTurns:                         10,
		Timeout:                          10 * time.Second,
		MaxTokens:                        4096,
		DisablePriorToolResultCompaction: false, // marker-based providers keep the shrink
	}
	streamer := runToolLoop(t, cfg, big, 3)

	mutated := false
	for turn := 1; turn < len(streamer.recordedMsgs); turn++ {
		if firstDivergentMessage(streamer.recordedMsgs[turn-1], streamer.recordedMsgs[turn]) >= 0 {
			mutated = true
			break
		}
	}
	if !mutated {
		t.Error("expected compaction (gate off) to rewrite a prior tool_result within the run, " +
			"but every request stayed a byte-prefix extension — the harness would miss a real mutation")
	}
}
