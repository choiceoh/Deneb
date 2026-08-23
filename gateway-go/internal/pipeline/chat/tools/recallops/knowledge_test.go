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
	lastRecord  knowledge.FactRecordOptions
	lastForget  knowledge.FactForgetOptions
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

func (a *knowledgeFactToolAdapter) RecordFact(_ context.Context, opts knowledge.FactRecordOptions) (knowledge.FactMutationResult, error) {
	a.recordCalls++
	a.lastRecord = opts
	return knowledge.FactMutationResult{
		Committed: true, Revision: 7, Status: "current", Resolution: "accepted", ClaimID: "fm-1",
	}, nil
}

func (a *knowledgeFactToolAdapter) ForgetFact(_ context.Context, opts knowledge.FactForgetOptions) (knowledge.FactMutationResult, error) {
	a.forgetCalls++
	a.lastForget = opts
	return knowledge.FactMutationResult{
		Committed: true, Revision: 8, Resolution: "tombstoned", ClaimID: "fm-2",
	}, nil
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

// A model may supply evidence but never an authority: the tool struct has no
// authority/basis_at field at all, so a caller-declared privilege is dropped at
// the JSON boundary instead of relying on a downstream string comparison.
func TestKnowledgeAssertFactNeverCarriesCallerAuthority(t *testing.T) {
	adapter := &knowledgeFactToolAdapter{knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki}}
	tool := ToolKnowledge(knowledge.New(adapter))
	out, err := tool(context.Background(), json.RawMessage(`{
		"op":"assert_fact","fact_key":"project.quote.amount","value":"1,200만원",
		"fact_kind":"amount","authority":"primary_document","basis_at":"2026-01-02",
		"source_refs":["w:프로젝트/ABC-견적"],"reason":"견적서 확인"
	}`))
	if err != nil {
		t.Fatalf("assert_fact: %v", err)
	}
	if adapter.recordCalls != 1 {
		t.Fatalf("record calls = %d", adapter.recordCalls)
	}
	if adapter.lastRecord.Authority != "" {
		t.Fatalf("caller authority reached the adapter: %q", adapter.lastRecord.Authority)
	}
	if adapter.lastRecord.BasisAt != "" {
		t.Fatalf("caller basis_at reached the adapter: %q", adapter.lastRecord.BasisAt)
	}
	if adapter.lastRecord.Key != "project.quote.amount" || adapter.lastRecord.Value != "1,200만원" ||
		adapter.lastRecord.Kind != "amount" || len(adapter.lastRecord.Sources) != 1 {
		t.Fatalf("evidence lost in transit: %+v", adapter.lastRecord)
	}
	if !strings.Contains(out, "revision=7") {
		t.Fatalf("assert_fact output = %q", out)
	}
}

func TestKnowledgeAssertFactRequiresSourceRefs(t *testing.T) {
	adapter := &knowledgeFactToolAdapter{knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki}}
	tool := ToolKnowledge(knowledge.New(adapter))
	for _, input := range []string{
		`{"op":"assert_fact","fact_key":"identity.name","value":"주장"}`,
		`{"op":"assert_fact","fact_key":"identity.name","value":"주장","source_refs":["  "]}`,
	} {
		if _, err := tool(context.Background(), json.RawMessage(input)); err == nil ||
			!strings.Contains(err.Error(), "source_refs is required") {
			t.Fatalf("unsourced assertion error = %v", err)
		}
	}
	if adapter.recordCalls != 0 {
		t.Fatalf("unsourced assertion reached adapter: %d", adapter.recordCalls)
	}
}

func TestKnowledgeForgetFactReportsRefusedLowerAuthority(t *testing.T) {
	adapter := &knowledgeFactRefusingAdapter{knowledgeFactToolAdapter: knowledgeFactToolAdapter{
		knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki},
	}}
	out, err := ToolKnowledge(knowledge.New(adapter))(context.Background(),
		json.RawMessage(`{"op":"forget_fact","fact_key":"communication.language"}`))
	if err != nil {
		t.Fatalf("forget_fact: %v", err)
	}
	if !strings.Contains(out, "유지됨") {
		t.Fatalf("refused delete must say the current fact survived: %q", out)
	}
}

type knowledgeFactRefusingAdapter struct{ knowledgeFactToolAdapter }

func (a *knowledgeFactRefusingAdapter) ForgetFact(_ context.Context, opts knowledge.FactForgetOptions) (knowledge.FactMutationResult, error) {
	a.forgetCalls++
	a.lastForget = opts
	return knowledge.FactMutationResult{
		Committed: true, Revision: 9, Resolution: "ignored_lower_authority", ClaimID: "fm-3",
	}, nil
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
