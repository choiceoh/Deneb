package skilllifecycle

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// The review runs with a dedicated lean system prompt instead of the full
// main-session assembly (context files, memory, skills index ≈ 50K tokens).
// Everything the run needs must therefore live in the system preamble or the
// review prompt itself.
func TestSkillReviewSystemPromptIncludesToolsWithinSizeBoundary(t *testing.T) {
	for _, want := range []string{"fetch_tools", "skills", "skill_lifecycle", "propose"} {
		if !strings.Contains(skillReviewSystemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	if len(skillReviewSystemPrompt) > 2000 {
		t.Errorf("system prompt should stay lean, got %d bytes", len(skillReviewSystemPrompt))
	}
}

// Without the main system prompt the skill index is no longer preloaded — the
// template must tell the reviewer to fetch it via the skills tool, and to fall
// back conservatively when the listing is empty/unavailable (nil snapshot
// after a restart, always-on skills missing from the discoverable list).
func TestBuildSkillReviewPromptIncludesIndexGuidanceAndFallback(t *testing.T) {
	prompt := buildSkillReviewPrompt("client:main", generation.SessionContext{AllText: "user: 테스트"}, "(none)")
	if !strings.Contains(prompt, "skills action=list") {
		t.Error("prompt missing skills-index guidance")
	}
	if !strings.Contains(prompt, "Do not call fetch_tools in the normal path") {
		t.Error("prompt missing direct preloaded-tool guidance")
	}
	if !strings.Contains(prompt, "decide conservatively") {
		t.Error("prompt missing empty-listing fallback discipline")
	}
	if !strings.Contains(prompt, "user: 테스트") {
		t.Error("prompt missing embedded transcript")
	}
}

type captureReviewChat struct {
	req chatport.SyncRequest
}

func (*captureReviewChat) ChatReady() bool { return true }

func (c *captureReviewChat) RunSync(_ context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	c.req = req
	return &chatport.SyncResult{StopReason: "end_turn"}, nil
}

func TestRunSkillReviewUsesBoundedNoThinkingPolicy(t *testing.T) {
	chat := &captureReviewChat{}
	fork := NewReviewFork(chat, nil, nil, "provider/coding-model", nil)

	if err := fork.RunSkillReview(context.Background(), "client:main", generation.SessionContext{
		AllText: "user: reusable workflow",
		Turns:   2,
	}); err != nil {
		t.Fatalf("RunSkillReview() error = %v", err)
	}

	if chat.req.SessionKey != "system:skill-review:client:main" {
		t.Fatalf("SessionKey = %q", chat.req.SessionKey)
	}
	if chat.req.Model != "provider/coding-model" || chat.req.ToolPreset != "self-review" {
		t.Fatalf("model/preset = %q/%q", chat.req.Model, chat.req.ToolPreset)
	}
	if chat.req.MaxTokens == nil || *chat.req.MaxTokens != skillReviewMaxTokens {
		t.Fatalf("MaxTokens = %v, want %d", chat.req.MaxTokens, skillReviewMaxTokens)
	}
	if chat.req.Thinking != skillReviewThinking {
		t.Fatalf("Thinking = %q, want %q", chat.req.Thinking, skillReviewThinking)
	}
	if chat.req.MaxHistoryTokens != skillReviewHistoryBudget || !chat.req.EphemeralUser || !chat.req.EphemeralAssistant {
		t.Fatalf("history/ephemeral policy = history %d user %v assistant %v",
			chat.req.MaxHistoryTokens, chat.req.EphemeralUser, chat.req.EphemeralAssistant)
	}
	if !strings.Contains(chat.req.SystemPrompt, "do not call fetch_tools unless one is unexpectedly unavailable") {
		t.Fatal("system prompt must steer away from the fetch_tools prelude")
	}
}

// The fork session key derives a persisted `system:skill-review:*` identity
// from arbitrary session keys — unsafe runes must be folded to '_'.
func TestSkillReviewSessionKeyNormalizesUnsafeRunes(t *testing.T) {
	cases := map[string]string{
		"client:main":     "system:skill-review:client:main",
		"cron:작업/1 테스트":   "system:skill-review:cron:작업_1_테스트",
		"a b\tc\nd(e)f.g": "system:skill-review:a_b_c_d_e_f_g",
	}
	for in, want := range cases {
		if got := skillReviewSessionKey(in); got != want {
			t.Errorf("skillReviewSessionKey(%q) = %q, want %q", in, got, want)
		}
	}
}

type timeoutReviewChat struct{}

func (*timeoutReviewChat) ChatReady() bool { return true }

func (*timeoutReviewChat) RunSync(_ context.Context, _ chatport.SyncRequest) (*chatport.SyncResult, error) {
	return &chatport.SyncResult{StopReason: "timeout", Turns: 3}, nil
}

// A review cut off at its budget recorded no lifecycle decision in the common
// case, so it must surface as a failed review — reporting nil here is what kept
// a 62% timeout rate invisible on /health.
func TestRunSkillReviewReportsBudgetTimeoutAsFailure(t *testing.T) {
	fork := NewReviewFork(&timeoutReviewChat{}, nil, nil, "provider/coding-model", nil)
	err := fork.RunSkillReview(context.Background(), "client:main", generation.SessionContext{AllText: "user: 테스트"})
	if err == nil {
		t.Fatal("a timed-out review must not report success")
	}
	if !strings.Contains(err.Error(), "budget elapsed") {
		t.Fatalf("error should name the budget, got %v", err)
	}
}
