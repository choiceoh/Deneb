package compaction

import (
	"encoding/json"
	"regexp"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// spilloverRefPattern matches the read_spillover pointer that
// ai/agent.TruncateHeadTail embeds when a large tool result is spilled
// to disk: `... [N lines truncated — use read_spillover("sp_abc123") for full
// content] ...`. The id is `sp_%x` (hex) — see agent.SpilloverStore.Store.
var spilloverRefPattern = regexp.MustCompile(`read_spillover\("(sp_[0-9a-fA-F]+)"\)`)

// spilloverRef returns the sp_ id in a tool result whose full output was
// spilled to disk, or "" if the result carries no such pointer. The cheap
// pruning passes (MicroCompact, TruncateOldToolResults) MUST preserve this
// pointer: the full output still lives in the spill file (cleaned only at
// session end), so stubbing or fence-stripping the pointer away strands it —
// the agent can no longer read_spillover the result it was told to page
// through. protectedToolResultIDs only covers fetch_tools by tool_use id; a
// spilled result is identified by this content marker instead.
func spilloverRef(content string) string {
	m := spilloverRefPattern.FindStringSubmatch(content)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

// protectedToolResultIDs collects the tool_use ids whose results must survive
// the cheap pruning passes (MicroCompact, TruncateOldToolResults).
//
// Currently protected:
//
//   - fetch_tools: its result IS the tool schema the model needs for the rest
//     of the run — pruning it forces an identical re-fetch a few turns later,
//     which the pruners then clear again. Production measurement (2026-07-05,
//     14d agent-logs): 20% of fetch_tools calls were same-input, same-output
//     repeats inside one run, up to 7 re-fetches in a single run.
//   - skills action=read/list: skill bodies and the manifest are deterministic
//     and re-consulted throughout a session under the JIT loading contract.
//     Production measurement (2026-07-20, 7d agent-logs): 38 of 73 skills
//     calls (52%) were identical re-executions more than 4 assistant turns
//     after the first call — the window where Tier 2b had already stubbed the
//     earlier result — and zero re-executions happened while the result was
//     still resident. Mutating actions (create/patch/delete/write_file/
//     remove_file) stay unprotected; their acks are small enough that the
//     pruners skip them anyway.
//
// The LLM summary tiers do not consult this list, so a very long session still
// bounds the resident cost of protected results.
func protectedToolResultIDs(messages []llm.Message) map[string]bool {
	var ids map[string]bool
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(m.Content.Bytes(), &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "tool_use" && b.ID != "" && isProtectedToolUse(b) {
				if ids == nil {
					ids = map[string]bool{}
				}
				ids[b.ID] = true
			}
		}
	}
	return ids
}

// isProtectedToolUse reports whether the result of this tool_use must survive
// the cheap pruning passes.
func isProtectedToolUse(b llm.ContentBlock) bool {
	switch b.Name {
	case "fetch_tools":
		return true
	case "skills":
		var in struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(b.Input.Bytes(), &in); err != nil {
			return false
		}
		return in.Action == "read" || in.Action == "list"
	default:
		return false
	}
}
