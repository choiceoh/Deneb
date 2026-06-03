package gmailpoll

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// TestAnalysisThinking locks the gating: extended thinking turns on ONLY for an
// Anthropic-mode client with real token headroom. OpenAI-mode (the local vLLM
// path that leaked CoT into the body, #1816) and tight budgets stay disabled.
func TestAnalysisThinking(t *testing.T) {
	anthropic := llm.NewClient("http://x", "k", llm.WithAPIMode(llm.APIModeAnthropic))
	openai := llm.NewClient("http://x", "k", llm.WithAPIMode(llm.APIModeOpenAI))

	cases := []struct {
		name   string
		client *llm.Client
		maxTok int
		wantOn bool
	}{
		{"anthropic + headroom → on", anthropic, 4096, true},
		{"anthropic + tight budget → off", anthropic, stage2MaxTokens, false},
		{"openai + headroom → off (vLLM leak guard)", openai, 4096, false},
		{"nil client → off", nil, 4096, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := analysisThinking(tc.client, tc.maxTok)
			on := got != nil && got.Type == "enabled"
			if on != tc.wantOn {
				t.Fatalf("analysisThinking on=%v, want %v (cfg=%+v)", on, tc.wantOn, got)
			}
			if on && got.BudgetTokens >= tc.maxTok {
				t.Fatalf("budget %d must be < maxTokens %d to leave room for the answer", got.BudgetTokens, tc.maxTok)
			}
		})
	}
}

func TestExtractDisplayName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"name + angle email", `홍길동 <hong@example.com>`, "홍길동"},
		{"quoted name", `"홍길동" <hong@example.com>`, "홍길동"},
		{"bare email", "hong@example.com", "hong@example.com"},
		{"empty", "", ""},
		{"only whitespace", "   ", ""},
		{"no name before angle", "<hong@example.com>", "hong@example.com"},
		{"name with parens kept", "Alice (ext) <a@x.com>", "Alice (ext)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractDisplayName(c.in); got != c.want {
				t.Errorf("extractDisplayName(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestExtractWikiGraphContext_NoSender(t *testing.T) {
	// Empty From → empty MemoryContext, never even tries to exec.
	msg := &gmail.MessageDetail{From: ""}
	got := extractWikiGraphContext(context.Background(), msg)
	if hasMemoryContext(got) {
		t.Errorf("expected empty MemoryContext for empty From, got %+v", got)
	}
}

func TestExtractWikiGraphContext_GracefulDegradation(t *testing.T) {
	// With a real sender but no graphify binary / no graph file in the test
	// environment, the function must return cleanly (empty MemoryContext) and
	// never panic — this guards the "best-effort, never blocks the pipeline"
	// contract that AnalyzeEmailPipeline relies on.
	msg := &gmail.MessageDetail{From: "홍길동 <hong@example.com>"}
	got := extractWikiGraphContext(context.Background(), msg)
	// Result depends on whether the test box happens to have ~/.deneb/wiki-graph
	// and graphify on PATH; either way the call must complete without panic.
	_ = got
}
