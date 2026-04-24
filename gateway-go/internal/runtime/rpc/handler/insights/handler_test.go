package insights

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type stubLister struct {
	items []*session.Session
}

func (s *stubLister) List() []*session.Session { return s.items }
func (s *stubLister) Count() int               { return len(s.items) }

func TestMethodsReturnsNilWhenEngineNil(t *testing.T) {
	got := Methods(Deps{Engine: nil})
	if got != nil {
		t.Fatalf("expected nil methods when engine is nil, got %+v", got)
	}
}

func TestInsightsGenerateDefaultsTo30Days(t *testing.T) {
	eng := insights.New(&stubLister{}, nil)
	methods := Methods(Deps{Engine: eng})
	fn := methods["insights.generate"]
	if fn == nil {
		t.Fatalf("insights.generate handler not registered")
	}

	resp := fn(context.Background(), &protocol.RequestFrame{ID: "req-1"})
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected error response: %+v", resp)
	}

	// Decode the response payload to verify Days=30 default was applied.
	var out GenerateResult
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if out.Report == nil || out.Report.Days != 30 {
		t.Errorf("expected Days=30; got %+v", out.Report)
	}
	if out.Markdown == "" {
		t.Errorf("expected MarkdownV2 output, got empty string")
	}
}

func TestInsightsGenerateCustomDays(t *testing.T) {
	eng := insights.New(&stubLister{}, nil)
	methods := Methods(Deps{Engine: eng})
	fn := methods["insights.generate"]

	params, _ := json.Marshal(GenerateParams{Days: 7})
	resp := fn(context.Background(), &protocol.RequestFrame{ID: "req-2", Params: params})
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp)
	}

	var out GenerateResult
	_ = json.Unmarshal(resp.Payload, &out)
	if out.Report == nil || out.Report.Days != 7 {
		t.Errorf("expected Days=7; got %+v", out.Report)
	}
}
