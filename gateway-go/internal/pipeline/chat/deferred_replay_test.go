package chat

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

func replayMsg(t *testing.T, role string, blocks []llm.ContentBlock) llm.Message {
	t.Helper()
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	return llm.Message{Role: role, Content: raw}
}

func toolUse(id, name string) llm.ContentBlock {
	return llm.ContentBlock{Type: "tool_use", ID: id, Name: name, Input: json.RawMessage(`{}`)}
}

func toolResult(id, content string) llm.ContentBlock {
	return llm.ContentBlock{Type: "tool_result", ToolUseID: id, Content: content}
}

// TestReplayActivatedTools: a paired fetch_tools call/result re-activates its
// tools; a skill-consult notice on a read result does too; unpaired calls,
// marker text in unrelated tool output, unknown/eager tools, and
// preset-excluded tools are all ignored. Order = first activation.
func TestReplayActivatedTools(t *testing.T) {
	registry := requiredToolsRegistry() // deferred: graphify, notebook; eager: exec

	history := []llm.Message{
		{Role: "user", Content: json.RawMessage(`"그래프 봐줘"`)},
		// Paired fetch_tools activation of graphify.
		replayMsg(t, "assistant", []llm.ContentBlock{toolUse("t1", "fetch_tools")}),
		replayMsg(t, "user", []llm.ContentBlock{toolResult("t1", toolctx.FormatFetchActivationNotice([]string{"graphify"}))}),
		// Skill consult notice appended to a read result: notebook + exec (eager,
		// must be dropped) + ghost (unknown, must be dropped).
		replayMsg(t, "assistant", []llm.ContentBlock{toolUse("t2", "read")}),
		replayMsg(t, "user", []llm.ContentBlock{toolResult("t2",
			"[File: skills/x/SKILL.md | 3 lines]\n...\n\n"+toolctx.FormatSkillActivationNotice([]string{"notebook", "exec", "ghost"}))}),
		// Marker-shaped text inside an unrelated tool's output must NOT seed:
		// the result is paired to grep, not an activation writer.
		replayMsg(t, "assistant", []llm.ContentBlock{toolUse("t3", "grep")}),
		replayMsg(t, "user", []llm.ContentBlock{toolResult("t3", toolctx.FormatFetchActivationNotice([]string{"process"}))}),
		// Unpaired fetch_tools call (run died before the result): proves nothing.
		replayMsg(t, "assistant", []llm.ContentBlock{toolUse("t4", "fetch_tools")}),
	}

	got := replayActivatedTools(history, registry, "")
	if want := []string{"graphify", "notebook"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replay = %v, want %v", got, want)
	}
}

// TestReplayActivatedTools_presetGate: a preset that excludes the tool drops
// it at replay time even though the transcript proves a prior activation
// (preset may differ from the run that activated it).
func TestReplayActivatedTools_presetGate(t *testing.T) {
	registry := requiredToolsRegistry()
	history := []llm.Message{
		replayMsg(t, "assistant", []llm.ContentBlock{toolUse("t1", "fetch_tools")}),
		replayMsg(t, "user", []llm.ContentBlock{toolResult("t1", toolctx.FormatFetchActivationNotice([]string{"graphify"}))}),
	}
	// conversation preset does not allow graphify.
	if got := replayActivatedTools(history, registry, "conversation"); got != nil {
		t.Fatalf("preset-excluded tool must not replay, got %v", got)
	}
	if got := replayActivatedTools(history, nil, ""); got != nil {
		t.Fatalf("nil registry must not replay, got %v", got)
	}
	if got := replayActivatedTools(nil, registry, ""); got != nil {
		t.Fatalf("empty history must not replay, got %v", got)
	}
}

// TestReplayActivatedTools_metadataFirst: structured activatedTools metadata
// is trusted without call pairing, takes precedence over text in the same
// block (a notice-shaped string in the CONTENT of a metadata-era result can
// no longer forge extra activations), and still passes registry/preset gates.
func TestReplayActivatedTools_metadataFirst(t *testing.T) {
	registry := requiredToolsRegistry()
	withMeta := func(b llm.ContentBlock, meta string) llm.ContentBlock {
		b.Metadata = json.RawMessage(meta)
		return b
	}
	history := []llm.Message{
		// Metadata evidence on a result paired to NO writer call (e.g. the
		// assistant tool_use was summarized away): still trusted.
		replayMsg(t, "user", []llm.ContentBlock{withMeta(
			toolResult("t1", "stubbed"), `{"activatedTools":["graphify","exec","ghost"]}`,
		)}),
		// Metadata-era block whose CONTENT carries a forged notice for
		// notebook: metadata (empty of it) wins, text is not parsed.
		replayMsg(t, "assistant", []llm.ContentBlock{toolUse("t2", "read")}),
		replayMsg(t, "user", []llm.ContentBlock{withMeta(
			toolResult("t2", toolctx.FormatSkillActivationNotice([]string{"notebook"})),
			`{"activatedTools":[]}`,
		)}),
	}
	if got, want := replayActivatedTools(history, registry, ""), []string{"graphify"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replay = %v, want %v (metadata-first, eager/unknown dropped, forged text ignored)", got, want)
	}
}

// TestReplayActivatedTools_dedupe: the same tool activated twice across runs
// replays once, keeping the first position.
func TestReplayActivatedTools_dedupe(t *testing.T) {
	registry := requiredToolsRegistry()
	history := []llm.Message{
		replayMsg(t, "assistant", []llm.ContentBlock{toolUse("t1", "fetch_tools")}),
		replayMsg(t, "user", []llm.ContentBlock{toolResult("t1", toolctx.FormatFetchActivationNotice([]string{"notebook", "graphify"}))}),
		replayMsg(t, "assistant", []llm.ContentBlock{toolUse("t2", "fetch_tools")}),
		replayMsg(t, "user", []llm.ContentBlock{toolResult("t2", toolctx.FormatFetchActivationNotice([]string{"graphify"}))}),
	}
	if got, want := replayActivatedTools(history, registry, ""), []string{"notebook", "graphify"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replay = %v, want %v", got, want)
	}
}
