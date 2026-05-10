package chat

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

// anthropicMaxBreakpoints is the documented Anthropic Messages API limit on
// cache_control markers per request. Sending more than this number causes
// the request to be rejected with a 400 error. The exact number is locked
// in this constant so a future Anthropic change is a single-edit follow-up
// rather than an invariant rewrite.
const anthropicMaxBreakpoints = 4

// countCacheBreakpoints reports the total number of cache_control markers
// across the system block array, the messages array, and the tools array
// of a request. This is the integration-scope view of the budget that
// Anthropic enforces per request.
func countCacheBreakpoints(sysBlocks []llm.ContentBlock, messages []llm.Message, tools []llm.Tool) int {
	count := 0
	for _, b := range sysBlocks {
		if b.CacheControl != nil {
			count++
		}
	}
	for _, m := range messages {
		var blocks []llm.ContentBlock
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			// String content carries no per-block markers; nothing to count.
			continue
		}
		for _, b := range blocks {
			if b.CacheControl != nil {
				count++
			}
		}
	}
	for _, t := range tools {
		if t.CacheControl != nil {
			count++
		}
	}
	return count
}

// applyTrailingHookOrIdentity runs buildTrailingCacheHook for the given
// apiMode if it is non-nil; otherwise returns messages unchanged. Mirrors
// the way ComposeBeforeAPICall threads nil hooks.
func applyTrailingHookOrIdentity(apiMode string, messages []llm.Message) []llm.Message {
	hook := buildTrailingCacheHook(apiMode)
	if hook == nil {
		return messages
	}
	return hook(messages)
}

func TestCacheBreakpointBudget_AnthropicWithSemiStatic(t *testing.T) {
	// Static + Semi-static + Dynamic (no marker) + msg×2 = 4 breakpoints.
	sysBlocks := prompt.BuildSystemPromptBlocks(prompt.SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []prompt.ToolDef{{Name: "read"}},
		SkillsPrompt: `<available_skills><skill><name>x</name></skill></available_skills>`,
	})
	msgs := []llm.Message{
		llm.NewTextMessage("user", "u1"),
		llm.NewTextMessage("assistant", "a1"),
		llm.NewTextMessage("user", "u2"),
	}
	msgs = applyTrailingHookOrIdentity(llm.APIModeAnthropic, msgs)

	got := countCacheBreakpoints(sysBlocks, msgs, nil)
	if got > anthropicMaxBreakpoints {
		t.Fatalf("breakpoint budget exceeded: got %d, max %d", got, anthropicMaxBreakpoints)
	}
	if got != 4 {
		t.Errorf("expected 4 breakpoints (Static + Semi + msg×2), got %d", got)
	}
}

func TestCacheBreakpointBudget_AnthropicWithEmptySkillsPrompt(t *testing.T) {
	// Even when SkillsPrompt="", buildPromptSections fills the semi-static
	// block with a default skills-discovery notice (system_prompt.go else
	// branch around line 267). So the system always carries 2 markers,
	// which combined with the 2 trailing message markers exactly fills
	// the 4-breakpoint budget.
	sysBlocks := prompt.BuildSystemPromptBlocks(prompt.SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []prompt.ToolDef{{Name: "read"}},
	})
	msgs := []llm.Message{
		llm.NewTextMessage("user", "u1"),
		llm.NewTextMessage("assistant", "a1"),
		llm.NewTextMessage("user", "u2"),
	}
	msgs = applyTrailingHookOrIdentity(llm.APIModeAnthropic, msgs)

	got := countCacheBreakpoints(sysBlocks, msgs, nil)
	if got > anthropicMaxBreakpoints {
		t.Fatalf("breakpoint budget exceeded: got %d, max %d", got, anthropicMaxBreakpoints)
	}
	if got != 4 {
		t.Errorf("expected 4 breakpoints (Static + Semi[default] + msg×2), got %d", got)
	}
}

func TestCacheBreakpointBudget_AnthropicSingleMessage(t *testing.T) {
	// Static + Semi + msg×1 = 3. The hook only marks one trailing message
	// when only one exists; the budget invariant still holds.
	sysBlocks := prompt.BuildSystemPromptBlocks(prompt.SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []prompt.ToolDef{{Name: "read"}},
		SkillsPrompt: `<available_skills><skill><name>x</name></skill></available_skills>`,
	})
	msgs := []llm.Message{llm.NewTextMessage("user", "alone")}
	msgs = applyTrailingHookOrIdentity(llm.APIModeAnthropic, msgs)

	got := countCacheBreakpoints(sysBlocks, msgs, nil)
	if got > anthropicMaxBreakpoints {
		t.Fatalf("budget exceeded: %d", got)
	}
	if got != 3 {
		t.Errorf("expected 3 breakpoints (Static + Semi + msg×1), got %d", got)
	}
}

func TestCacheBreakpointBudget_OpenAIHookSkips(t *testing.T) {
	// Non-Anthropic providers must not gain trailing markers. System
	// markers (Static + Semi-default) stay attached because the system
	// blocks are built once at assembly time without knowing the wire
	// protocol — non-Anthropic providers ignore them on the wire.
	sysBlocks := prompt.BuildSystemPromptBlocks(prompt.SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []prompt.ToolDef{{Name: "read"}},
	})
	msgs := []llm.Message{
		llm.NewTextMessage("user", "u1"),
		llm.NewTextMessage("assistant", "a1"),
	}
	msgs = applyTrailingHookOrIdentity(llm.APIModeOpenAI, msgs)

	got := countCacheBreakpoints(sysBlocks, msgs, nil)
	if got > anthropicMaxBreakpoints {
		t.Fatalf("budget exceeded: %d", got)
	}
	if got != 2 {
		t.Errorf("expected 2 breakpoints (Static + Semi[default], no trailing), got %d", got)
	}
}

func TestCacheBreakpointBudget_AnthropicMultiBlockMessages(t *testing.T) {
	// User messages can carry multi-block content (text + image, or
	// tool_result blocks). The trailing hook attaches the marker to the
	// LAST block of the LAST 2 non-system messages — total still <= 4.
	sysBlocks := prompt.BuildSystemPromptBlocks(prompt.SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []prompt.ToolDef{{Name: "read"}},
		SkillsPrompt: `<available_skills><skill><name>x</name></skill></available_skills>`,
	})
	msgs := []llm.Message{
		llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "text", Text: "first u"},
			{Type: "text", Text: "second"},
		}),
		llm.NewBlockMessage("assistant", []llm.ContentBlock{
			{Type: "text", Text: "thinking"},
			{Type: "tool_use", ID: "t1", Name: "read", Input: json.RawMessage(`{}`)},
		}),
		llm.NewBlockMessage("user", []llm.ContentBlock{
			{Type: "tool_result", ToolUseID: "t1", Content: "ok"},
		}),
	}
	msgs = applyTrailingHookOrIdentity(llm.APIModeAnthropic, msgs)

	got := countCacheBreakpoints(sysBlocks, msgs, nil)
	if got > anthropicMaxBreakpoints {
		t.Fatalf("budget exceeded: got %d, max %d", got, anthropicMaxBreakpoints)
	}
	if got != 4 {
		t.Errorf("expected 4 breakpoints (Static + Semi + msg×2 last-block), got %d", got)
	}
}
