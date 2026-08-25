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
// store, survives gateway restarts). Evidence has two tiers: the structured
// activatedTools metadata the executor attaches to tool_result blocks
// (pkg/toolmeta — server-attached, so tool output CONTENT cannot forge it),
// and, for pre-metadata transcripts, the exact activation notices the writers
// emit (toolport/activation_notice.go), parsed only from results paired by
// tool_use_id to an activating tool's call — so a marker-shaped string inside
// some unrelated tool output (a read of a log file, promptware) cannot seed
// tools, and an unpaired call (run died before the result) proves nothing.
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
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
	"github.com/choiceoh/deneb/gateway-go/pkg/toolmeta"
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

	// Pass 1: map tool_use_id → name for calls made by activating tools —
	// only the TEXT fallback needs this pairing; the metadata path below is
	// server-attached and trusts the block directly. The same walk records
	// every tool the model actually CALLED, which gates the replay below.
	writerCalls := make(map[string]bool)
	calledTools := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, b := range decodeBlocks(json.RawMessage(msg.Content.Bytes())) {
			if b.Type != "tool_use" {
				continue
			}
			calledTools[b.Name] = true
			if activationNoticeWriters[b.Name] {
				writerCalls[b.ID] = true
			}
		}
	}

	// Pass 2: collect activation evidence from tool_results, keeping order.
	// Metadata first — the activatedTools sideband is attached by the
	// executor (pkg/toolmeta), so tool output CONTENT cannot forge it and no
	// call pairing is needed. The text-notice parse remains as the fallback
	// for pre-metadata transcripts, gated to writer-paired results as before.
	allowed := toolwire.AllowedTools(sessionToolPreset)
	seen := make(map[string]bool)
	var names []string
	admit := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		// Activated but never called: let it lapse. Activation used to be
		// permanent for the life of the transcript, so one exploratory
		// fetch_tools kept re-shipping that tool's whole schema on every later
		// turn — measured 2026-08-26 in a puppet session: 12 replayed tools,
		// 24,225 of the request's 69,202 schema bytes (35%), while only three
		// were ever called (skill_lifecycle alone was 11,947 bytes). A lapsed
		// tool costs one cheap re-fetch: the deferred catalog is still listed
		// in the system prompt with its fetch_tools pointer. (Operator call,
		// 2026-08-26: "실제 호출된 도구만 리플레이".)
		if !calledTools[name] {
			return
		}
		if _, ok := registry.DeferredToolDef(name); !ok {
			return
		}
		if allowed != nil {
			if _, ok := allowed[name]; !ok {
				return
			}
		}
		names = append(names, name)
	}
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		for _, b := range decodeBlocks(json.RawMessage(msg.Content.Bytes())) {
			if b.Type != "tool_result" {
				continue
			}
			var metaNames []string
			if toolmeta.Get(json.RawMessage(b.Metadata.Bytes()), "activatedTools", &metaNames) {
				for _, name := range metaNames {
					admit(name)
				}
				continue
			}
			if !writerCalls[b.ToolUseID] {
				continue
			}
			for _, name := range toolport.ParseActivationNotices(b.Content) {
				admit(name)
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
