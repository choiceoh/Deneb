package runtimeops

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
			"mail_archive": {Name: "mail_archive", Description: "Read local mail archive", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"names": []string{"mail_archive"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, out, "mail_archive")
}

func TestFetchTools_ByQuery(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"mail_archive": {Name: "mail_archive", Description: "Read email from the local archive", Deferred: true},
			"storage":      {Name: "storage", Description: "Object storage", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "email"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, out, "mail_archive")
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

// Substring fallback fires when no whole token matches.
func TestFetchTools_ByQuery_SubstringFallback(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"notebook": {Name: "notebook", Description: "Deal notes", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "book"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, out, "notebook")
}

// A whole-token BM25 hit and a substring-only match are both surfaced (recall
// floor preserved).
func TestFetchTools_ByQuery_UnionBM25AndSubstring(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"book":     {Name: "book", Description: "book tool", Deferred: true},
			"notebook": {Name: "notebook", Description: "notes", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "book"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, out, "book")     // exact-token BM25 hit
	assertActivated(t, out, "notebook") // substring-only match, unioned in
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

// Under an active tool preset, fetch_tools must not activate (or even
// surface) deferred tools outside the preset's allow-list — otherwise a
// restricted sub-agent gets told "you can now call them directly" about a
// tool Execute will reject.
func TestFetchTools_PresetBlocksDisallowedName(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"cron":         {Name: "cron", Description: "Schedule recurring jobs", Deferred: true},
			"mail_archive": {Name: "mail_archive", Description: "Read local mail archive", Deferred: true},
			"send_file":    {Name: "send_file", Description: "Send a file to the user", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	ctx := toolctx.WithToolPreset(context.Background(), "researcher")

	out, err := fn(ctx, mustJSON(t, map[string]any{"names": []string{"cron", "send_file", "mail_archive"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "- cron: not available under the current tool preset") {
		t.Fatalf("expected cron blocked under researcher preset, got: %s", out)
	}
	if !strings.Contains(out, "- send_file: not available under the current tool preset") {
		t.Fatalf("expected send_file blocked under researcher preset, got: %s", out)
	}
	// mail_archive IS in the researcher allow-list — must still activate.
	assertActivated(t, out, "mail_archive")
}

// A tool already activated in a prior turn (visible via the DeferredActivation
// snapshot) is short-circuited: no schema re-emit, just an "already active"
// pointer. A not-yet-active sibling in the same call still gets its schema.
func TestFetchTools_AlreadyActiveShortCircuit(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"mail_archive": {Name: "mail_archive", Description: "Read local mail archive", Deferred: true},
			"cron":         {Name: "cron", Description: "Schedule recurring jobs", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)

	da := toolctx.NewDeferredActivation()
	ctx := toolctx.WithDeferredActivation(context.Background(), da)

	// Turn N: activate mail_archive; executor drains between turns.
	out, err := fn(ctx, mustJSON(t, map[string]any{"names": []string{"mail_archive"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, out, "mail_archive")
	da.ActivatedNames() // executor drain publishes the active snapshot

	// Turn N+1: re-fetch mail_archive plus a new tool.
	out, err = fn(ctx, mustJSON(t, map[string]any{"names": []string{"mail_archive", "cron"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "## mail_archive") {
		t.Fatalf("expected no schema re-emit for already-active tool, got: %s", out)
	}
	if !strings.Contains(out, "Already active") || !strings.Contains(out, "mail_archive") {
		t.Fatalf("expected already-active pointer for mail_archive, got: %s", out)
	}
	assertActivated(t, out, "cron")
}

// Without an executor drain in between, a same-turn duplicate still returns
// the schema (the snapshot only updates between turns) — documented tradeoff.
func TestFetchTools_SameTurnDuplicateStillReturnsSchema(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"mail_archive": {Name: "mail_archive", Description: "Read local mail archive", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)

	da := toolctx.NewDeferredActivation()
	ctx := toolctx.WithDeferredActivation(context.Background(), da)

	if _, err := fn(ctx, mustJSON(t, map[string]any{"names": []string{"mail_archive"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, err := fn(ctx, mustJSON(t, map[string]any{"names": []string{"mail_archive"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, out, "mail_archive")
}

func TestFetchTools_PresetFiltersQueryResults(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolctx.ToolDef{
			"cron": {Name: "cron", Description: "Schedule recurring jobs", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	ctx := toolctx.WithToolPreset(context.Background(), "researcher")

	out, err := fn(ctx, mustJSON(t, map[string]any{"query": "schedule recurring"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No deferred tools match") {
		t.Fatalf("expected disallowed tool hidden from query results, got: %s", out)
	}
}
