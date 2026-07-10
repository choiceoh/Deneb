// deferred_replay.go — carry deferred-tool activation across runs by replaying
// transcript evidence (Pydantic AI's load_capability replay discipline).
//
// DeferredActivation is run-scoped: without replay, a tool the model activated
// via fetch_tools (or a skill consult) vanishes from the Tools array on the
// next user message — while the transcript still tells the model "call them
// directly". The model then wastes a turn re-fetching, and the Tools array
// shrinking back also breaks the provider prompt-cache prefix that the
// previous run's requests established.
//
// Replay re-derives the activated set from the transcript itself (no separate
// store, survives gateway restarts): scan assistant tool_use blocks for the
// activating tools, pair them with their tool_result by tool_use_id, and parse
// the exact activation notices those writers emit
// (toolctx/activation_notice.go). Pairing is deliberate — a result is only
// trusted if it belongs to an activating tool's call, so a marker-shaped
// string inside some unrelated tool output (a read of a log file, promptware)
// cannot seed tools. An unpaired call (run died before the result) proves
// nothing and is ignored.
//
// Cheap pruning keeps the evidence alive: fetch_tools results are on the
// compaction protection list, and TruncateOldToolResults re-embeds activation
// notices into its stubs (compaction/restore.go). Only the LLM-summary tier
// can still summarize evidence away, and a tool may have been removed or
// preset-excluded since — then the name simply doesn't re-activate and the
// model re-fetches like today. Graceful degradation, never an error.
package chat

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
)

// activationNoticeWriters are the tools whose results can carry an activation
// notice: fetch_tools emits the activation sentence itself; read and skills
// get the skill-consult notice appended (tool_skill_required_tools.go).
var activationNoticeWriters = map[string]bool{
	"fetch_tools": true,
	"read":        true,
	"skills":      true,
}

// replayActivatedTools scans an assembled message history for deferred-tool
// activation evidence and returns the names that are still valid to activate
// (deferred in the registry, allowed by the run's preset), in first-activation
// order and deduplicated. Returns nil when there is nothing to replay.
func replayActivatedTools(messages []llm.Message, registry *ToolRegistry, sessionToolPreset string) []string {
	if registry == nil || len(messages) == 0 {
		return nil
	}

	// Pass 1: map tool_use_id → name for calls made by activating tools.
	writerCalls := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, b := range decodeBlocks(msg.Content) {
			if b.Type == "tool_use" && activationNoticeWriters[b.Name] {
				writerCalls[b.ID] = true
			}
		}
	}
	if len(writerCalls) == 0 {
		return nil
	}

	// Pass 2: parse notices out of the paired tool_results, keeping order.
	allowed := toolpreset.AllowedTools(toolpreset.Preset(sessionToolPreset))
	seen := make(map[string]bool)
	var names []string
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, b := range decodeBlocks(msg.Content) {
			if b.Type != "tool_result" || !writerCalls[b.ToolUseID] {
				continue
			}
			for _, name := range toolctx.ParseActivationNotices(b.Content) {
				if seen[name] {
					continue
				}
				seen[name] = true
				if _, ok := registry.DeferredToolDef(name); !ok {
					continue
				}
				if allowed != nil {
					if _, ok := allowed[name]; !ok {
						continue
					}
				}
				names = append(names, name)
			}
		}
	}
	return names
}

// decodeBlocks returns the ContentBlocks of a rich-format message, or nil for
// text-form (JSON string) content.
func decodeBlocks(content json.RawMessage) []llm.ContentBlock {
	if len(content) == 0 || content[0] != '[' {
		return nil
	}
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(content, &blocks); err != nil {
		return nil
	}
	return blocks
}
