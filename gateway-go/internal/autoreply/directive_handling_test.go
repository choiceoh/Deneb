package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"strings"
	"testing"
)

func TestHandleDirectives_ThinkOnly(t *testing.T) {
	result := HandleDirectives("/think high", nil, DirectiveHandlingOptions{})
	if result.SessionMod == nil {
		t.Fatal("expected session mod")
	}
	if result.SessionMod.ThinkLevel != types.ThinkHigh {
		t.Errorf("think = %q", result.SessionMod.ThinkLevel)
	}
	if result.IsDirectiveOnly != true {
		t.Error("expected directive-only")
	}
}

func TestHandleDirectives_MultipleWithText(t *testing.T) {
	result := HandleDirectives("hello /think medium /fast", nil, DirectiveHandlingOptions{})
	if result.IsDirectiveOnly {
		t.Error("should not be directive-only")
	}
	if result.SessionMod == nil {
		t.Fatal("expected session mod")
	}
	if result.SessionMod.ThinkLevel != types.ThinkMedium {
		t.Errorf("think = %q", result.SessionMod.ThinkLevel)
	}
	if !strings.Contains(result.CleanedBody, "hello") {
		t.Errorf("cleaned = %q", result.CleanedBody)
	}
}

func TestHandleDirectives_ModelWithCandidates(t *testing.T) {
	candidates := []ModelCandidate{
		{Provider: "anthropic", Model: "claude-3", Label: "Claude 3"},
		{Provider: "openai", Model: "gpt-4", Label: "GPT-4"},
	}
	result := HandleDirectives("/model gpt-4", nil, DirectiveHandlingOptions{
		ModelCandidates: candidates,
	})
	if result.ModelResolution == nil {
		t.Fatal("expected model resolution")
	}
	if !result.ModelResolution.IsValid {
		t.Error("expected valid resolution")
	}
	if result.ModelResolution.Model != "gpt-4" {
		t.Errorf("model = %q", result.ModelResolution.Model)
	}
}

func TestHandleDirectives_ElevatedBlocked(t *testing.T) {
	result := HandleDirectives("/elevated on", nil, DirectiveHandlingOptions{
		RequireAuthForElevated: true,
		IsAuthorized:           false,
	})
	if len(result.Errors) == 0 {
		t.Error("expected auth error")
	}
}

func TestPersistDirectives(t *testing.T) {
	session := &types.SessionState{}
	result := DirectiveHandlingResult{
		SessionMod: &SessionModification{
			ThinkLevel: types.ThinkHigh,
			FastMode:   boolPtr(true),
		},
	}
	PersistDirectives(session, result)
	if session.ThinkLevel != types.ThinkHigh {
		t.Errorf("think = %q", session.ThinkLevel)
	}
	if !session.FastMode {
		t.Error("expected fast mode on")
	}
}

func TestIsFastLaneDirective(t *testing.T) {
	d := ParseInlineDirectives("/think high", nil)
	if !IsFastLaneDirective(d) {
		t.Error("expected fast lane")
	}

	d = ParseInlineDirectives("/model gpt-4", nil)
	if IsFastLaneDirective(d) {
		t.Error("model change should not be fast lane")
	}
}

func TestBuildFastLaneReply(t *testing.T) {
	d := ParseInlineDirectives("/think high /fast", nil)
	reply := BuildFastLaneReply(d)
	if reply == nil {
		t.Fatal("expected reply")
	}
	if !strings.Contains(reply.Text, "Think") {
		t.Errorf("reply should mention Think: %q", reply.Text)
	}
}

func TestParseDirectiveParams(t *testing.T) {
	params := ParseDirectiveParams("hello @key=value @flag world")
	if params.Params["key"] != "value" {
		t.Errorf("key = %q", params.Params["key"])
	}
	if params.Params["flag"] != "true" {
		t.Errorf("flag = %q", params.Params["flag"])
	}
}

func boolPtr(b bool) *bool { return &b }
