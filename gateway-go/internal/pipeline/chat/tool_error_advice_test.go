package chat

import "testing"

// toolErrorAdvisorShape mirrors the agent executor's unexported optional
// capability. The executor discovers it by type assertion, so a rename here
// (or there) silently disables error-time correction memory instead of failing
// the build — this assertion is what makes that a compile error.
type toolErrorAdvisorShape interface {
	ToolErrorAdvice(toolName, errText string) string
}

var _ toolErrorAdvisorShape = (*ToolRegistry)(nil)

// Without a wired advisor the registry must stay silent; with one, the lookup
// receives the tool name and error text verbatim.
func TestToolRegistryErrorAdvice(t *testing.T) {
	r := NewToolRegistry()
	if got := r.ToolErrorAdvice("gmail", "label not found"); got != "" {
		t.Errorf("unwired registry advised %q, want silence", got)
	}

	var gotTool, gotErr string
	r.SetToolErrorAdvisor(func(toolName, errText string) string {
		gotTool, gotErr = toolName, errText
		return "이전에 scope 필드를 고쳐 성공했다"
	})
	if got := r.ToolErrorAdvice("gmail", "label not found"); got == "" {
		t.Error("wired advisor produced no advice")
	}
	if gotTool != "gmail" || gotErr != "label not found" {
		t.Errorf("advisor saw (%q, %q), want (gmail, label not found)", gotTool, gotErr)
	}
}
