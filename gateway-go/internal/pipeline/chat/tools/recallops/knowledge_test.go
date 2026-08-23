package recallops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/knowledge"
)

type knowledgeToolAdapter struct {
	layer   knowledge.Layer
	results []knowledge.Result
}

type knowledgeFactToolAdapter struct {
	knowledgeToolAdapter
	factSubject string
	factKey     string
	factViews   []knowledge.FactView
	recordCalls int
	forgetCalls int
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

func (a *knowledgeFactToolAdapter) RecordFact(context.Context, knowledge.FactRecordOptions) (knowledge.FactMutationResult, error) {
	a.recordCalls++
	return knowledge.FactMutationResult{}, nil
}

func (a *knowledgeFactToolAdapter) ForgetFact(context.Context, knowledge.FactForgetOptions) (knowledge.FactMutationResult, error) {
	a.forgetCalls++
	return knowledge.FactMutationResult{}, nil
}

func (a *knowledgeFactToolAdapter) Facts(_ context.Context, subject, key string) ([]knowledge.FactView, error) {
	a.factSubject, a.factKey = subject, key
	return a.factViews, nil
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

func TestKnowledgeFactMutationsAreNotModelCallable(t *testing.T) {
	adapter := &knowledgeFactToolAdapter{knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki}}
	tool := ToolKnowledge(knowledge.New(adapter))
	for _, input := range []string{
		`{"op":"assert_fact","fact_key":"identity.name","value":"external claim"}`,
		`{"op":"forget_fact","fact_key":"identity.name"}`,
	} {
		_, err := tool(context.Background(), json.RawMessage(input))
		if err == nil || !strings.Contains(err.Error(), "unknown knowledge op") {
			t.Fatalf("disabled mutation error = %v", err)
		}
	}
	if adapter.recordCalls != 0 || adapter.forgetCalls != 0 {
		t.Fatalf("disabled fact mutation reached adapter: record=%d forget=%d", adapter.recordCalls, adapter.forgetCalls)
	}
}

func TestKnowledgeFactsRendersStableAuditJSON(t *testing.T) {
	adapter := &knowledgeFactToolAdapter{
		knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki},
		factViews: []knowledge.FactView{{
			ID: "claim-9", Subject: "project:deneb", Key: "owner", Value: "김 대리",
			Kind: "generic", Authority: "primary_document", Status: "current",
			Sources: []string{"doc:org-chart"}, Actor: "knowledge-tool", Reason: "org chart",
			RecordedAtMs: 1234, BasisAtMs: 1200, Revision: 9,
		}},
	}
	got, err := ToolKnowledge(knowledge.New(adapter))(context.Background(), json.RawMessage(`{
		"op":"facts","subject":"project:deneb","fact_key":"owner"
	}`))
	if err != nil {
		t.Fatalf("facts: %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(got), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("facts JSON = %q, err = %v", got, err)
	}
	for _, key := range []string{"id", "subject", "key", "value", "sources", "actor", "reason", "recordedAtMs", "basisAtMs", "revision"} {
		if _, ok := rows[0][key]; !ok {
			t.Errorf("facts JSON missing %q: %s", key, got)
		}
	}
	if _, leakedGoName := rows[0]["RecordedAtMs"]; leakedGoName {
		t.Fatalf("facts JSON leaked Go field names: %s", got)
	}
	if adapter.factSubject != "project:deneb" || adapter.factKey != "owner" {
		t.Fatalf("captured query = %q/%q", adapter.factSubject, adapter.factKey)
	}
}

func TestKnowledgeFactsCapsOutputAndKeepsNewestHistory(t *testing.T) {
	views := make([]knowledge.FactView, 60)
	for i := range views {
		views[i] = knowledge.FactView{ID: fmt.Sprintf("claim-%d", i+1), Revision: uint64(i + 1)}
	}
	adapter := &knowledgeFactToolAdapter{
		knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki},
		factViews:            views,
	}
	tool := ToolKnowledge(knowledge.New(adapter))

	got, err := tool(context.Background(), json.RawMessage(`{
		"op":"facts","subject":"self","fact_key":"communication.response_length","limit":3
	}`))
	if err != nil {
		t.Fatalf("facts with limit: %v", err)
	}
	var rows []knowledge.FactView
	if err := json.Unmarshal([]byte(got), &rows); err != nil {
		t.Fatalf("facts JSON = %q, err = %v", got, err)
	}
	if len(rows) != 3 || rows[0].Revision != 58 || rows[2].Revision != 60 {
		t.Fatalf("limited history = %+v", rows)
	}

	got, err = tool(context.Background(), json.RawMessage(`{
		"op":"facts","subject":"self","fact_key":"communication.response_length"
	}`))
	if err != nil {
		t.Fatalf("facts with default cap: %v", err)
	}
	if err := json.Unmarshal([]byte(got), &rows); err != nil {
		t.Fatalf("default-capped facts JSON = %q, err = %v", got, err)
	}
	if len(rows) != knowledgeFactsDefaultLimit || rows[0].Revision != 51 || rows[len(rows)-1].Revision != 60 {
		t.Fatalf("default-capped history = len %d first %d last %d", len(rows), rows[0].Revision, rows[len(rows)-1].Revision)
	}
}
