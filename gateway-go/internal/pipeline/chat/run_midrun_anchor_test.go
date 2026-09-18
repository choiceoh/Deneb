package chat

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func toolResultsMsg(extraText ...string) llm.Message {
	blocks := []llm.ContentBlock{{Type: "tool_result", ToolUseID: "t1", Content: "[ctx] [assistant] 과거 대화 발췌 …"}}
	for _, t := range extraText {
		blocks = append(blocks, llm.ContentBlock{Type: "text", Text: t})
	}
	return llm.NewBlockMessage("user", blocks)
}

func blocksOf(t *testing.T, msg llm.Message) []llm.ContentBlock {
	t.Helper()
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(msg.Content.Bytes(), &blocks); err != nil {
		t.Fatalf("block content expected: %v", err)
	}
	return blocks
}

// Mid-run step: the last message is the executor's tool-results user message,
// so the anchor rides it as a trailing text block — after any notices the
// executor already attached — and the input is not mutated.
func TestAppendMidRunAnchorRidesToolResultsMessage(t *testing.T) {
	in := []llm.Message{
		llm.NewTextMessage("user", "질문"),
		llm.NewBlockMessage("assistant", []llm.ContentBlock{{Type: "tool_use", ID: "t1", Name: "sessions"}}),
		toolResultsMsg("**System:** subagent completed."),
	}
	before := in[2].Content.String()
	out := appendMidRunAnchor(in, responseLanguageAnchor)
	blocks := blocksOf(t, out[2])
	if len(blocks) != 3 || blocks[2].Type != "text" || blocks[2].Text != responseLanguageAnchor {
		t.Fatalf("anchor not appended as the trailing text block: %+v", blocks)
	}
	if blocks[1].Text != "**System:** subagent completed." {
		t.Fatalf("existing notice displaced: %+v", blocks)
	}
	if in[2].Content.String() != before {
		t.Fatal("input message mutated (wire-only contract)")
	}
	// Idempotent when re-applied to the already-anchored view.
	if again := appendMidRunAnchor(out, responseLanguageAnchor); len(blocksOf(t, again[2])) != 3 {
		t.Fatal("anchor duplicated on re-application")
	}
}

// First step (the tail anchor already sits in the user message) and any
// non-user tail pass through untouched — the hook is strictly for tool steps.
func TestAppendMidRunAnchorLeavesOtherTailsAlone(t *testing.T) {
	cases := map[string][]llm.Message{
		"real user turn (string)": {llm.NewTextMessage("user", "질문\n\n"+responseLanguageAnchor)},
		"real user turn (blocks, no tool_result)": {llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "text", Text: "사진 봐줘"}, {Type: "image", Source: &llm.ImageSource{Type: "base64", MediaType: "image/png", Data: "x"}},
		})},
		"assistant tail": {llm.NewTextMessage("user", "질문"), llm.NewTextMessage("assistant", "답")},
		"empty":          {},
	}
	for name, in := range cases {
		out := appendMidRunAnchor(in, responseLanguageAnchor)
		if len(out) != len(in) {
			t.Fatalf("%s: length changed", name)
		}
		for i := range in {
			if out[i].Content.String() != in[i].Content.String() {
				t.Fatalf("%s: message %d changed", name, i)
			}
		}
	}
}

// Gating: ephemeral turns get no hook at all; the env kill switch turns it off
// for everyone else. Content-prefix providers (kimi — main since 2026-09-18)
// are NOT excluded: the per-request block is not a history mutation.
func TestBuildMidRunAnchorHookGates(t *testing.T) {
	if buildMidRunAnchorHook(RunParams{EphemeralUser: true}, nil) != nil {
		t.Error("ephemeral turn must not carry the mid-run anchor")
	}
	t.Setenv(midRunAnchorEnv, "off")
	if buildMidRunAnchorHook(RunParams{}, nil) != nil {
		t.Error("DENEB_MIDRUN_ANCHOR=off must disable the hook")
	}
	t.Setenv(midRunAnchorEnv, "")
	if buildMidRunAnchorHook(RunParams{}, nil) == nil {
		t.Error("interactive turn must carry the hook")
	}
}

// Composed after the trailing cache hook (Anthropic mode): the cache_control
// marker stays on the clean tool-result block and the anchor block carries
// none, so the marker budget is unchanged and the cached prefix is the same
// bytes the next step reproduces.
func TestMidRunAnchorRunsAfterTrailingCacheMarker(t *testing.T) {
	in := []llm.Message{
		llm.NewTextMessage("user", "질문"),
		llm.NewBlockMessage("assistant", []llm.ContentBlock{{Type: "tool_use", ID: "t1", Name: "wiki"}}),
		toolResultsMsg(),
	}
	var chain agent.BeforeAPICallChain
	chain.Add("trailing-cache", agent.HookStagePost, buildTrailingCacheHook("anthropic"))
	chain.Add("midrun-anchor", agent.HookStagePost, buildMidRunAnchorHook(RunParams{}, nil), "trailing-cache")
	out := chain.Build(nil)(in)
	blocks := blocksOf(t, out[2])
	if len(blocks) != 2 || blocks[1].Text != responseLanguageAnchor {
		t.Fatalf("anchor missing from the tool-results tail: %+v", blocks)
	}
	if blocks[0].CacheControl == nil {
		t.Fatal("trailing cache marker must stay on the clean tool_result block")
	}
	if blocks[1].CacheControl != nil {
		t.Fatal("the anchor block must not carry a cache_control marker")
	}
	total := 0
	for _, m := range out {
		var bs []llm.ContentBlock
		if json.Unmarshal(m.Content.Bytes(), &bs) != nil {
			continue // plain-string message: no blocks, no markers
		}
		for _, b := range bs {
			if b.CacheControl != nil {
				total++
			}
		}
	}
	if total > 2 {
		t.Fatalf("trailing markers = %d, want ≤ 2 (message budget half of the 4-marker rule)", total)
	}
}
