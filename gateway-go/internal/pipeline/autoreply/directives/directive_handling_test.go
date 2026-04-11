package directives

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"testing"
)

func TestHandleDirectives_FastOnly(t *testing.T) {
	result := HandleDirectives("/fast", nil, DirectiveHandlingOptions{})
	if result.SessionMod == nil {
		t.Fatal("expected session mod")
	}
	if result.SessionMod.FastMode == nil || !*result.SessionMod.FastMode {
		t.Error("expected fast mode on")
	}
	if result.IsDirectiveOnly != true {
		t.Error("expected directive-only")
	}
}

func TestHandleDirectives_MultipleWithText(t *testing.T) {
	result := HandleDirectives("hello /verbose full /fast", nil, DirectiveHandlingOptions{})
	if result.IsDirectiveOnly {
		t.Error("should not be directive-only")
	}
	if result.SessionMod == nil {
		t.Fatal("expected session mod")
	}
	if result.SessionMod.VerboseLevel != types.VerboseFull {
		t.Errorf("verbose = %q", result.SessionMod.VerboseLevel)
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
	sess := &types.SessionState{}
	result := DirectiveHandlingResult{
		SessionMod: &types.SessionModification{
			FastMode: boolPtr(true),
		},
	}
	PersistDirectives(sess, result)
	if !sess.FastMode {
		t.Error("expected fast mode on")
	}
}

func boolPtr(b bool) *bool { return &b }
