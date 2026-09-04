package chat

import (
	"context"
	"encoding/json"
	"testing"
)

const signedThinkingBlocks = `[{"type":"thinking","thinking":"reasoning in english","signature":"sig-abc"},` +
	`{"type":"text","text":"답"}]`

func decodeBlocksForTest(t *testing.T, raw json.RawMessage) []map[string]any {
	t.Helper()
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("blocks did not survive as JSON: %v", err)
	}
	return blocks
}

func TestAttachThinkingDisplayKeepsTheBlockIntact(t *testing.T) {
	deps := runDeps{translateThinking: func(_ context.Context, text string) (string, bool) {
		if text != "reasoning in english" {
			t.Fatalf("translator got %q", text)
		}
		return "영어로 사고함", true
	}}

	blocks := decodeBlocksForTest(t, attachThinkingDisplay(deps, json.RawMessage(signedThinkingBlocks), nil))
	if len(blocks) != 2 {
		t.Fatalf("block count = %d, want 2", len(blocks))
	}
	// The model's own text and its provider signature must survive untouched —
	// the replay that feeds the reasoning back depends on both.
	if got := blocks[0]["thinking"]; got != "reasoning in english" {
		t.Fatalf("thinking text was rewritten: %v", got)
	}
	if got := blocks[0]["signature"]; got != "sig-abc" {
		t.Fatalf("signature lost: %v", got)
	}
	meta, ok := blocks[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("no metadata attached: %v", blocks[0])
	}
	if meta[thinkingDisplayKey] != "영어로 사고함" {
		t.Fatalf("display copy = %v, want the translation", meta[thinkingDisplayKey])
	}
	if _, has := blocks[1]["metadata"]; has {
		t.Fatal("a text block was given a reasoning display copy")
	}
}

func TestAttachThinkingDisplayFailsOpen(t *testing.T) {
	cases := map[string]struct {
		deps runDeps
		raw  string
	}{
		"no translator wired": {
			deps: runDeps{},
			raw:  signedThinkingBlocks,
		},
		"translator refuses": {
			deps: runDeps{translateThinking: func(context.Context, string) (string, bool) { return "", false }},
			raw:  signedThinkingBlocks,
		},
		"translator returns blank": {
			deps: runDeps{translateThinking: func(context.Context, string) (string, bool) { return "  ", true }},
			raw:  signedThinkingBlocks,
		},
		"content is not a block array": {
			deps: runDeps{translateThinking: func(context.Context, string) (string, bool) { return "번역", true }},
			raw:  `"a plain string message"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := attachThinkingDisplay(tc.deps, json.RawMessage(tc.raw), nil)
			if string(got) != tc.raw {
				t.Fatalf("content was altered on a path that must pass through:\n got %s\nwant %s", got, tc.raw)
			}
		})
	}
}

func TestAttachThinkingDisplayMergesIntoExistingMetadata(t *testing.T) {
	// metadata is a shared sideband (pkg/toolmeta writes there too); adding the
	// display copy must not evict what is already in it.
	const withMeta = `[{"type":"thinking","thinking":"english","metadata":{"activatedTools":["graphify"]}}]`
	deps := runDeps{translateThinking: func(context.Context, string) (string, bool) { return "한국어", true }}

	blocks := decodeBlocksForTest(t, attachThinkingDisplay(deps, json.RawMessage(withMeta), nil))
	meta, _ := blocks[0]["metadata"].(map[string]any)
	if meta[thinkingDisplayKey] != "한국어" {
		t.Fatalf("display copy missing: %v", meta)
	}
	if _, has := meta["activatedTools"]; !has {
		t.Fatalf("existing metadata was evicted: %v", meta)
	}
}

func TestAttachThinkingDisplayHonoursItsDeadline(t *testing.T) {
	// A cold cache must not hold the agent loop between turns: the translator is
	// handed a bounded context and the row is persisted without a copy.
	var deadlineSeen bool
	deps := runDeps{translateThinking: func(ctx context.Context, _ string) (string, bool) {
		if _, ok := ctx.Deadline(); ok {
			deadlineSeen = true
		}
		return "", false
	}}
	got := attachThinkingDisplay(deps, json.RawMessage(signedThinkingBlocks), nil)
	if !deadlineSeen {
		t.Fatal("translator was called without a deadline")
	}
	if string(got) != signedThinkingBlocks {
		t.Fatal("a refused translation must leave the persisted content alone")
	}
}
