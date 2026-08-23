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
	recordedFact knowledge.FactRecordOptions
	forgotten    knowledge.FactForgetOptions
	factSubject  string
	factKey      string
	mutation     knowledge.FactMutationResult
	factViews    []knowledge.FactView
	recordCalls  int
	forgetCalls  int
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
	a.recordedFact = opts
	return a.mutation, nil
}

func (a *knowledgeFactToolAdapter) ForgetFact(_ context.Context, opts knowledge.FactForgetOptions) (knowledge.FactMutationResult, error) {
	a.forgetCalls++
	a.forgotten = opts
	return a.mutation, nil
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

func TestKnowledgeFactToolRejectsDirectUserAuthorityBeforeAdapter(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "assert", input: `{"op":"assert_fact","fact_key":"identity.name","value":"external claim","authority":"direct_user"}`},
		{name: "forget", input: `{"op":"forget_fact","fact_key":"identity.name","authority":"DIRECT_USER"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &knowledgeFactToolAdapter{knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki}}
			_, err := ToolKnowledge(knowledge.New(adapter))(context.Background(), json.RawMessage(tc.input))
			if err == nil || !strings.Contains(err.Error(), "reserved for trusted direct-message induction") {
				t.Fatalf("error = %v, want direct_user boundary", err)
			}
			if adapter.recordCalls != 0 || adapter.forgetCalls != 0 {
				t.Fatalf("untrusted direct_user reached adapter: record=%d forget=%d", adapter.recordCalls, adapter.forgetCalls)
			}
		})
	}
}

func TestKnowledgeFactMutationRequiresAuthorityEvidence(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError string
	}{
		{
			name:      "primary document source",
			input:     `{"op":"assert_fact","fact_key":"owner","value":"Kim","authority":"primary_document"}`,
			wantError: "source_refs is required",
		},
		{
			name:      "runtime observation source",
			input:     `{"op":"assert_fact","fact_key":"service.state","value":"ready","authority":"runtime_observation","source_refs":[" "]}`,
			wantError: "source_refs is required",
		},
		{
			name:      "tombstone authority source",
			input:     `{"op":"forget_fact","fact_key":"service.state","authority":"runtime_observation"}`,
			wantError: "source_refs is required",
		},
		{
			name:      "dated primary amount",
			input:     `{"op":"assert_fact","fact_key":"quote.amount","value":"1000","fact_kind":"amount","authority":"primary_document","source_refs":["doc:q"]}`,
			wantError: "basis_at is required",
		},
		{
			name:      "dated primary deadline",
			input:     `{"op":"assert_fact","fact_key":"quote.deadline","value":"Friday","fact_kind":"deadline","authority":"primary_document","source_refs":["doc:q"]}`,
			wantError: "basis_at is required",
		},
		{
			name:      "dated primary contract",
			input:     `{"op":"assert_fact","fact_key":"contract.term","value":"net 30","fact_kind":"contract","authority":"primary_document","source_refs":["doc:c"]}`,
			wantError: "basis_at is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &knowledgeFactToolAdapter{knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki}}
			_, err := ToolKnowledge(knowledge.New(adapter))(context.Background(), json.RawMessage(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want %q", err, tc.wantError)
			}
			if adapter.recordCalls != 0 || adapter.forgetCalls != 0 {
				t.Fatalf("invalid authority evidence reached adapter: record=%d forget=%d", adapter.recordCalls, adapter.forgetCalls)
			}
		})
	}
}

func TestKnowledgeAssertFactDispatchesTypedFieldsAndReportsResolution(t *testing.T) {
	adapter := &knowledgeFactToolAdapter{
		knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki},
		mutation: knowledge.FactMutationResult{
			Revision: 4, ClaimID: "claim-4", Status: "current", Resolution: "latest_authoritative", Committed: true,
		},
	}
	tool := ToolKnowledge(knowledge.New(adapter))
	got, err := tool(context.Background(), json.RawMessage(`{
		"op":"assert_fact","subject":"project:deneb","fact_key":"quote.amount","value":"1200만원",
		"fact_kind":"amount","authority":"primary_document","source_refs":["doc:quote-2"],
		"basis_at":"2026-08-22","reason":"revised quote"
	}`))
	if err != nil {
		t.Fatalf("assert_fact: %v", err)
	}
	if !strings.Contains(got, "현행 사실 반영됨") || !strings.Contains(got, "revision=4") {
		t.Fatalf("current result = %q", got)
	}
	if adapter.recordedFact.Subject != "project:deneb" || adapter.recordedFact.Key != "quote.amount" ||
		adapter.recordedFact.Kind != "amount" || adapter.recordedFact.Authority != "primary_document" ||
		adapter.recordedFact.BasisAt != "2026-08-22" || len(adapter.recordedFact.Sources) != 1 {
		t.Fatalf("captured options = %+v", adapter.recordedFact)
	}

	adapter.mutation = knowledge.FactMutationResult{
		Revision: 5, ClaimID: "claim-5", Status: "superseded", Resolution: "ignored_lower_authority", Committed: true,
	}
	got, err = tool(context.Background(), json.RawMessage(`{
		"op":"assert_fact","fact_key":"quote.amount","value":"900만원","authority":"inference"
	}`))
	if err != nil {
		t.Fatalf("ignored assert_fact: %v", err)
	}
	if strings.Contains(got, "현행 사실 반영됨") || !strings.Contains(got, "현행 사실은 변경되지 않음") {
		t.Fatalf("superseded result overclaimed current state: %q", got)
	}

	adapter.mutation = knowledge.FactMutationResult{Resolution: "not_written"}
	if _, err := tool(context.Background(), json.RawMessage(`{
		"op":"assert_fact","fact_key":"quote.amount","value":"800만원","authority":"inference"
	}`)); err == nil || !strings.Contains(err.Error(), "not committed") {
		t.Fatalf("non-committed mutation error = %v", err)
	}
}

func TestKnowledgeForgetFactReportsAbsentTombstoneBarrier(t *testing.T) {
	adapter := &knowledgeFactToolAdapter{
		knowledgeToolAdapter: knowledgeToolAdapter{layer: knowledge.LayerWiki},
		mutation: knowledge.FactMutationResult{
			Revision: 8, ClaimID: "tombstone-8", Status: "tombstoned", Resolution: "already_absent", Committed: true,
		},
	}
	got, err := ToolKnowledge(knowledge.New(adapter))(context.Background(), json.RawMessage(`{
		"op":"forget_fact","subject":"self","fact_key":"diet.vegan","authority":"agent_confirmed",
		"source_refs":["session:client:main#30"],"reason":"requested"
	}`))
	if err != nil {
		t.Fatalf("forget_fact: %v", err)
	}
	if !strings.Contains(got, "활성 사실은 없었으며 tombstone 장벽을 기록함") {
		t.Fatalf("already-absent result = %q", got)
	}
	if adapter.forgotten.Key != "diet.vegan" || adapter.forgotten.Reason != "requested" || len(adapter.forgotten.Sources) != 1 {
		t.Fatalf("captured options = %+v", adapter.forgotten)
	}

	adapter.mutation = knowledge.FactMutationResult{
		Revision: 9, ClaimID: "tombstone-9", Status: "superseded", Resolution: "ignored_lower_authority", Committed: true,
	}
	got, err = ToolKnowledge(knowledge.New(adapter))(context.Background(), json.RawMessage(`{
		"op":"forget_fact","subject":"self","fact_key":"diet.vegan","authority":"inference"
	}`))
	if err != nil {
		t.Fatalf("ignored forget_fact: %v", err)
	}
	if strings.Contains(got, "소프트 삭제함") || !strings.Contains(got, "현행 사실은 유지됨") {
		t.Fatalf("ignored lower-authority tombstone overclaimed deletion: %q", got)
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
