package compaction

import (
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// protectedToolResultIDs collects the tool_use ids whose results must survive
// the cheap pruning passes (MicroCompact, TruncateOldToolResults).
//
// Currently protected: fetch_tools. Its result IS the tool schema the model
// needs for the rest of the run — pruning it forces an identical re-fetch a
// few turns later, which the pruners then clear again. Production measurement
// (2026-07-05, 14d agent-logs): 20% of fetch_tools calls were same-input,
// same-output repeats inside one run, up to 7 re-fetches in a single
// email-analysis run.
func protectedToolResultIDs(messages []llm.Message) map[string]bool {
	var ids map[string]bool
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "tool_use" && b.Name == "fetch_tools" && b.ID != "" {
				if ids == nil {
					ids = map[string]bool{}
				}
				ids[b.ID] = true
			}
		}
	}
	return ids
}
