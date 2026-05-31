package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// fakeFetchRegistry implements FetchToolsRegistry, mirroring the real
// chat.ToolRegistry: DeferredToolDef/DeferredSummaries only surface tools that
// are Deferred and not Hidden, so tests exercise a realistic catalog.
type fakeFetchRegistry struct {
	defs map[string]toolctx.ToolDef
}

func (f *fakeFetchRegistry) DeferredToolDef(name string) (toolctx.ToolDef, bool) {
	d, ok := f.defs[name]
	if !ok || !d.Deferred {
		return toolctx.ToolDef{}, false
	}
	return d, true
}

func (f *fakeFetchRegistry) DeferredSummaries() []toolctx.DeferredToolSummary {
	var out []toolctx.DeferredToolSummary
	for _, d := range f.defs {
		if d.Deferred && !d.Hidden {
			out = append(out, toolctx.DeferredToolSummary{Name: d.Name, Description: d.Description})
		}
	}
	// Stable order so map iteration doesn't make tests flaky.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
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

// assertActivated checks that a tool was actually found and activated (its
// schema header "## <name>" is present) rather than merely mentioned in a
// "- <name>: not found" error bullet.
func assertActivated(t *testing.T, out, name string) {
	t.Helper()
	if !strings.Contains(out, "## "+name) {
		t.Fatalf("expected %q to be activated (schema header), got: %s", name, out)
	}
	if strings.Contains(out, "- "+name+": not found") {
		t.Fatalf("expected %q activated but got not-found bullet: %s", name, out)
	}
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
	assertActivated(t, out, "gmail")
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
	assertActivated(t, out, "gmail")
	if strings.Contains(out, "## storage") {
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
	assertActivated(t, out, "storage")
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
	assertActivated(t, out, "gmail")
}

// A whole-token BM25 hit and a substring-only match are both surfaced (recall
// floor preserved): query "mail" should activate both a "mail" tool and "gmail".
func TestFetchTools_ByQuery_UnionBM25AndSubstring(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"mail":  {Name: "mail", Description: "mail tool", Deferred: true},
			"gmail": {Name: "gmail", Description: "google account", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "mail"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, out, "mail")  // exact-token BM25 hit
	assertActivated(t, out, "gmail") // substring-only match, unioned in
}

// A whitespace-only query is rejected just like an empty one.
func TestFetchTools_BlankQueryRejected(t *testing.T) {
	reg := &fakeFetchRegistry{defs: map[string]toolctx.ToolDef{}}
	fn := ToolFetchTools(reg)
	if _, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "   "})); err == nil {
		t.Fatalf("expected error for whitespace-only query")
	}
}

// Non-deferred tools are not surfaced by query search.
func TestFetchTools_NonDeferredExcluded(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"read": {Name: "read", Description: "read a file", Deferred: false},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "read"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No deferred tools match") {
		t.Fatalf("expected no-match for non-deferred tool, got: %s", out)
	}
}
