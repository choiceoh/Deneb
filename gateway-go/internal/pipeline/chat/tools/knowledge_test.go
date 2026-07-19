package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
)

type knowledgeToolAdapter struct {
	layer   knowledge.Layer
	results []knowledge.Result
}

func (a *knowledgeToolAdapter) Layer() knowledge.Layer { return a.layer }

func (a *knowledgeToolAdapter) Recall(context.Context, string, int) ([]knowledge.Result, error) {
	return a.results, nil
}

func (a *knowledgeToolAdapter) Read(context.Context, string) (*knowledge.Document, error) {
	return nil, nil
}

func (a *knowledgeToolAdapter) Descriptor() knowledge.SourceDescriptor {
	return knowledge.SourceDescriptor{Layer: a.layer, Name: "wiki"}
}

func TestKnowledgeRecallRendersExactLocatorAndLateContext(t *testing.T) {
	adapter := &knowledgeToolAdapter{layer: knowledge.LayerWiki, results: []knowledge.Result{{
		Ref:     knowledge.Ref{Layer: knowledge.LayerWiki, ID: "프로젝트/검색.md"},
		Snippet: "정확한 근거 ORBIT-7319",
		Context: "이전 설명\n\n정확한 근거 ORBIT-7319\n\n후속 설명",
		Score:   0.9,
		Provenance: knowledge.Provenance{Locator: knowledge.Locator{
			StartLine: 8, EndLine: 9, ContextStartLine: 4, ContextEndLine: 13,
		}},
	}}}
	router := knowledge.New(adapter)
	input := json.RawMessage(`{"op":"recall","query":"ORBIT-7319","sources":["wiki"],"scopes":["프로젝트"]}`)

	got, err := ToolKnowledge(router)(context.Background(), input)
	if err != nil {
		t.Fatalf("knowledge recall: %v", err)
	}
	for _, want := range []string{"w:프로젝트/검색.md", "L8-L9", "인접 문맥 L4-L13", "이전 설명", "후속 설명", "sources=[wiki]"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestKnowledgeRecallRejectsUnknownExplicitSource(t *testing.T) {
	_, err := ToolKnowledge(knowledge.New(&knowledgeToolAdapter{layer: knowledge.LayerWiki}))(
		context.Background(), json.RawMessage(`{"op":"recall","query":"x","sources":["slack"]}`),
	)
	if err == nil || !strings.Contains(err.Error(), "unknown knowledge source") {
		t.Fatalf("error = %v, want unknown source guidance", err)
	}
}
