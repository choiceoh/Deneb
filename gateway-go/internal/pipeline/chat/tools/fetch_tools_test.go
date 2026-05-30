package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// fakeFetchRegistry implements FetchToolsRegistry for tests.
type fakeFetchRegistry struct {
	defs map[string]toolctx.ToolDef
}

func (f *fakeFetchRegistry) DeferredToolDef(name string) (toolctx.ToolDef, bool) {
	d, ok := f.defs[name]
	return d, ok
}

func (f *fakeFetchRegistry) DeferredSummaries() []toolctx.DeferredToolSummary {
	var out []toolctx.DeferredToolSummary
	for _, d := range f.defs {
		out = append(out, toolctx.DeferredToolSummary{Name: d.Name, Description: d.Description})
	}
	return out
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestFetchTools_ByName(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"gmail": {Name: "gmail", Description: "Gmail access", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"names": []string{"gmail"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "gmail") {
		t.Fatalf("expected gmail in output, got: %s", out)
	}
}

func TestFetchTools_ByQuery(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"gmail":   {Name: "gmail", Description: "Send and read email", Deferred: true},
			"storage": {Name: "storage", Description: "Object storage", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "email"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "gmail") {
		t.Fatalf("expected gmail for email query, got: %s", out)
	}
	if strings.Contains(out, "storage") {
		t.Fatalf("did not expect storage for email query, got: %s", out)
	}
}

// Query matches a parameter name (not the name/description) via BM25 indexing.
func TestFetchTools_ByQuery_ParamName(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"storage": {
				Name:        "storage",
				Description: "Object store",
				Deferred:    true,
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"bucket": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "bucket"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "storage") {
		t.Fatalf("expected storage to match param-name query, got: %s", out)
	}
}

// Substring fallback fires when no whole token matches (e.g. "mail" -> "gmail").
func TestFetchTools_ByQuery_SubstringFallback(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"gmail": {Name: "gmail", Description: "Send and read email", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "mail"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "gmail") {
		t.Fatalf("expected substring fallback to match gmail, got: %s", out)
	}
}
