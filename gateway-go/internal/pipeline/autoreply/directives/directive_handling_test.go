package directives

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
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
	candidates := []model.ModelCandidate{
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

func TestPersistDirectives(t *testing.T) {
	session := &types.SessionState{}
	result := DirectiveHandlingResult{
		SessionMod: &types.SessionModification{
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

func boolPtr(b bool) *bool { return &b }
