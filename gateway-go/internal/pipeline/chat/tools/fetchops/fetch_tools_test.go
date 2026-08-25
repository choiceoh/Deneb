package fetchops

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/toolmeta"
)

// fakeFetchRegistry implements FetchToolsRegistry, mirroring the real
// chat.ToolRegistry: DeferredToolDef/DeferredSummaries only surface tools that
// are Deferred and not Hidden, so tests exercise a realistic catalog.
type fakeFetchRegistry struct {
	defs         map[string]toolport.ToolDef
	revision     uint64
	summaryCalls int
	toolDefCalls int
}

type fetchSemanticEmbedder struct {
	mu          sync.Mutex
	kinds       []string
	fingerprint string
}

type fetchTestReranker struct {
	calls int
	err   error
}

func (r *fetchTestReranker) Rerank(_ context.Context, _ string, documents []string) ([]float64, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	scores := make([]float64, len(documents))
	for i, document := range documents {
		if strings.Contains(document, "preferred candidate") {
			scores[i] = 10
		} else {
			scores[i] = -float64(i)
		}
	}
	return scores, nil
}

func (e *fetchSemanticEmbedder) IsHealthy() bool { return true }

func (e *fetchSemanticEmbedder) EmbeddingFingerprint() string { return e.fingerprint }

func (e *fetchSemanticEmbedder) EmbeddingDimensions() int { return 2 }

func (e *fetchSemanticEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.kinds = append(e.kinds, "passage")
	e.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if strings.Contains(text, "email") {
			out[i] = []float32{1, 0}
		} else {
			out[i] = []float32{0, 1}
		}
	}
	return out, nil
}

func (e *fetchSemanticEmbedder) EmbedKind(_ context.Context, kind string, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.kinds = append(e.kinds, kind)
	e.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if strings.Contains(text, "받은 편지함") {
			out[i] = []float32{1, 0}
		} else {
			out[i] = []float32{0, 1}
		}
	}
	return out, nil
}

func (e *fetchSemanticEmbedder) snapshotKinds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.kinds...)
}

func (f *fakeFetchRegistry) DeferredToolDef(name string) (toolport.ToolDef, bool) {
	f.toolDefCalls++
	d, ok := f.defs[name]
	if !ok || !d.Deferred {
		return toolport.ToolDef{}, false
	}
	return d, true
}

func (f *fakeFetchRegistry) DeferredSummaries() []toolport.DeferredToolSummary {
	f.summaryCalls++
	var out []toolport.DeferredToolSummary
	for _, d := range f.defs {
		if d.Deferred && !d.Hidden {
			out = append(out, toolport.DeferredToolSummary{Name: d.Name, Description: d.Description})
		}
	}
	// Stable order so map iteration doesn't make tests flaky.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (f *fakeFetchRegistry) DeferredCatalogRevision() uint64 {
	return f.revision
}

func (f *fakeFetchRegistry) registerForTest(def toolport.ToolDef) {
	f.defs[def.Name] = def
	f.revision++
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

func TestFetchTools_ByNameReturnsActivatedTool(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
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

func TestFetchTools_ByQueryReturnsMatchingTool(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
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

func TestFetchTools_ByQueryFindsSemanticOnlyToolAndCachesCatalog(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
			"mail_archive": {Name: "mail_archive", Description: "Read email from the local archive", Deferred: true},
			"storage":      {Name: "storage", Description: "Manage object buckets", Deferred: true},
		},
	}
	embedder := &fetchSemanticEmbedder{}
	fn := ToolFetchTools(reg, embedder)
	input := mustJSON(t, map[string]any{"query": "받은 편지함에서 대화 찾기"})

	for range 2 {
		out, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("semantic fetch: %v", err)
		}
		assertActivated(t, out, "mail_archive")
		if strings.Contains(out, "## storage") {
			t.Fatalf("unexpected semantic tool: %s", out)
		}
	}
	if want := []string{"passage", "query", "query"}; !slices.Equal(embedder.snapshotKinds(), want) {
		t.Fatalf("embedding roles/cache = %v, want %v", embedder.snapshotKinds(), want)
	}
}

func TestFetchTools_QuerySearchCatalogCachesAndInvalidatesByRevision(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
			"mail_archive": {Name: "mail_archive", Description: "Read email from the local archive", Deferred: true},
			"storage":      {Name: "storage", Description: "Manage object buckets", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	input := mustJSON(t, map[string]any{"query": "not-present"})

	for range 2 {
		out, err := fn(context.Background(), input)
		if err != nil {
			t.Fatalf("fetch query: %v", err)
		}
		if !strings.Contains(out, "No deferred tools match") {
			t.Fatalf("expected no-match output, got: %s", out)
		}
	}
	if reg.summaryCalls != 1 {
		t.Fatalf("DeferredSummaries calls = %d, want 1", reg.summaryCalls)
	}
	if reg.toolDefCalls != 2 {
		t.Fatalf("DeferredToolDef calls = %d, want 2", reg.toolDefCalls)
	}

	reg.registerForTest(toolport.ToolDef{
		Name:        "param_tool",
		Description: "Parameter-only search target",
		Deferred:    true,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"needle_param": map[string]any{"type": "string"},
			},
		},
	})
	out, err := fn(context.Background(), mustJSON(t, map[string]any{"query": "needle_param"}))
	if err != nil {
		t.Fatalf("fetch after revision change: %v", err)
	}
	assertActivated(t, out, "param_tool")
	if reg.summaryCalls != 2 {
		t.Fatalf("DeferredSummaries calls after invalidation = %d, want 2", reg.summaryCalls)
	}
}

func TestFetchTools_QueryRerankerPromotesCandidateFromWiderPool(t *testing.T) {
	reg := &fakeFetchRegistry{defs: map[string]toolport.ToolDef{}}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		reg.defs[name] = toolport.ToolDef{Name: name, Description: "email helper", Deferred: true}
	}
	reg.defs["z"] = toolport.ToolDef{Name: "z", Description: "email preferred candidate", Deferred: true}
	reranker := &fetchTestReranker{}

	out, err := ToolFetchToolsWithReranker(reg, nil, reranker)(context.Background(), mustJSON(t, map[string]any{"query": "email"}))
	if err != nil {
		t.Fatalf("reranked fetch: %v", err)
	}
	if reranker.calls != 1 {
		t.Fatalf("reranker calls = %d, want 1", reranker.calls)
	}
	assertActivated(t, out, "z")
	if got := strings.Count(out, "## "); got != searchResultLimit {
		t.Fatalf("activated schemas = %d, want %d: %s", got, searchResultLimit, out)
	}
}

func TestFetchTools_RerankerFailsOpenAndExplicitNamesBypassIt(t *testing.T) {
	reg := &fakeFetchRegistry{defs: map[string]toolport.ToolDef{
		"mail_archive": {Name: "mail_archive", Description: "email archive", Deferred: true},
		"storage":      {Name: "storage", Description: "email object storage", Deferred: true},
	}}
	input := mustJSON(t, map[string]any{"query": "email"})
	baseline, err := ToolFetchTools(reg)(context.Background(), input)
	if err != nil {
		t.Fatalf("baseline fetch: %v", err)
	}
	reranker := &fetchTestReranker{err: errors.New("sidecar unavailable")}
	out, err := ToolFetchToolsWithReranker(reg, nil, reranker)(context.Background(), input)
	if err != nil {
		t.Fatalf("fail-open fetch: %v", err)
	}
	if out != baseline {
		t.Fatalf("reranker failure changed output:\n--- got ---\n%s\n--- want ---\n%s", out, baseline)
	}

	if _, err := ToolFetchToolsWithReranker(reg, nil, reranker)(context.Background(), mustJSON(t, map[string]any{"names": []string{"mail_archive"}})); err != nil {
		t.Fatalf("explicit fetch: %v", err)
	}
	if reranker.calls != 1 {
		t.Fatalf("explicit names called reranker: calls=%d", reranker.calls)
	}
}

func TestFuseFetchToolRanksRejectsLowSemanticOnlyTail(t *testing.T) {
	floor := embedindex.CalibrationFor(nil, embedindex.SemanticSurfaceFetchTools).Floor
	got := fuseFetchToolRanks(
		[]string{"lexical"},
		[]semanticToolHit{{name: "lexical", score: 0.2}, {name: "noise", score: floor - 0.01}},
	)
	if !slices.Equal(got, []string{"lexical"}) {
		t.Fatalf("fused names = %v", got)
	}
}

func TestFetchToolsUnknownEmbedderDoesNotAdmitSemanticOnlyTool(t *testing.T) {
	reg := &fakeFetchRegistry{defs: map[string]toolport.ToolDef{
		"mail_archive": {Name: "mail_archive", Description: "Read email from the local archive", Deferred: true},
		"storage":      {Name: "storage", Description: "Manage object buckets", Deferred: true},
	}}
	embedder := &fetchSemanticEmbedder{fingerprint: "future-embedder:2"}
	out, err := ToolFetchTools(reg, embedder)(context.Background(), mustJSON(t, map[string]any{"query": "받은 편지함에서 대화 찾기"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "## mail_archive") {
		t.Fatalf("uncalibrated model admitted semantic-only tool: %s", out)
	}
}

// Query matches a parameter name (not the name/description) via BM25 indexing.
func TestFetchTools_ByQuery_ParamNameReturnsMatch(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
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
		defs: map[string]toolport.ToolDef{
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
func TestFetchTools_ByQuery_ReturnsUnionOfBM25AndSubstringMatches(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
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

func TestFetchTools_RequestValidationErrors(t *testing.T) {
	reg := &fakeFetchRegistry{defs: map[string]toolport.ToolDef{}}
	fn := ToolFetchTools(reg)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		input   json.RawMessage
		wantErr string
	}{
		{name: "nil context precedes parsing", input: json.RawMessage(`{"names":`), wantErr: "fetch_tools requires a context"},
		{name: "canceled context precedes parsing", ctx: canceled, input: json.RawMessage(`{"names":`), wantErr: "context canceled"},
		{name: "malformed JSON", ctx: context.Background(), input: json.RawMessage(`{"names":`), wantErr: "parse fetch_tools params: unexpected end of JSON input"},
		{name: "missing selector", ctx: context.Background(), input: json.RawMessage(`{}`), wantErr: "names or query is required"},
		{name: "blank query", ctx: context.Background(), input: mustJSON(t, map[string]any{"query": "   "}), wantErr: "names or query is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := fn(tt.ctx, tt.input)
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
			if out != "" {
				t.Fatalf("output = %q, want empty on validation error", out)
			}
		})
	}
}

// Non-deferred tools are not surfaced by query search.
func TestFetchTools_QueryIgnoresNonDeferredTools(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
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
		defs: map[string]toolport.ToolDef{
			"cron":         {Name: "cron", Description: "Schedule recurring jobs", Deferred: true},
			"mail_archive": {Name: "mail_archive", Description: "Read local mail archive", Deferred: true},
			"send_file":    {Name: "send_file", Description: "Send a file to the user", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	ctx := toolport.WithToolPreset(context.Background(), "researcher")

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
		defs: map[string]toolport.ToolDef{
			"mail_archive": {Name: "mail_archive", Description: "Read local mail archive", Deferred: true},
			"cron":         {Name: "cron", Description: "Schedule recurring jobs", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)

	da := toolport.NewDeferredActivation()
	ctx := toolport.WithDeferredActivation(context.Background(), da)

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

// A caller with no DeferredActivation cannot receive the tool through the tools
// array, so the result text stays the only channel and must keep the schema.
func TestFetchTools_InlinesSchemaWhenActivationIsUnavailable(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
			"mail_archive": {
				Name:        "mail_archive",
				Description: "Read mail",
				Deferred:    true,
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	out, err := ToolFetchTools(reg)(context.Background(), mustJSON(t, map[string]any{
		"names": []string{"mail_archive"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "```json") || !strings.Contains(out, "\"query\"") {
		t.Fatalf("schema must stay inline without activation, got:\n%s", out)
	}
}

func TestFetchTools_MixedSelectionReportsSchemaActivationAndErrors(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
			"contacts": {
				Name:        "contacts",
				Description: "Find people",
				Deferred:    true,
			},
			"cron": {
				Name:        "cron",
				Description: "Schedule jobs",
				Deferred:    true,
			},
			"mail_archive": {
				Name:        "mail_archive",
				Description: "Read mail",
				Deferred:    true,
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
					"required": []string{"query"},
				},
			},
		},
	}
	activation := toolport.NewDeferredActivation()
	activation.Seed([]string{"contacts"})
	collector := toolmeta.NewCollector()
	ctx := toolport.WithToolPreset(context.Background(), "researcher")
	ctx = toolport.WithDeferredActivation(ctx, activation)
	ctx = toolmeta.WithCollector(ctx, collector)

	out, err := ToolFetchTools(reg)(ctx, mustJSON(t, map[string]any{
		"names": []string{"cron", "graphify", "contacts", "mail_archive"},
		"query": "schedule jobs",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No inline JSON schema: activation is live, so the tools array carries it.
	// Inlining it too shipped every activated schema twice and left the copy in
	// the transcript for the rest of the session.
	want := "" +
		"- cron: not available under the current tool preset\n" +
		"- graphify: not found or not a deferred tool\n" +
		"## mail_archive\n" +
		"Read mail\n" +
		"\n" +
		"Already active (schema loaded, no re-fetch needed): contacts. Call them directly.\n" +
		"Activated 1 tool(s): mail_archive. You can now call them directly."
	if out != want {
		t.Fatalf("output:\n--- got ---\n%s\n--- want ---\n%s", out, want)
	}

	if got := activation.ActivatedNames(); !slices.Equal(got, []string{"contacts", "mail_archive"}) {
		t.Fatalf("activated names = %v, want [contacts mail_archive]", got)
	}
	var metadataNames []string
	if !toolmeta.Get(collector.JSON(), "activatedTools", &metadataNames) {
		t.Fatalf("activatedTools metadata missing: %s", collector.JSON())
	}
	if !slices.Equal(metadataNames, []string{"mail_archive", "contacts"}) {
		t.Fatalf("activatedTools metadata = %v, want [mail_archive contacts]", metadataNames)
	}
}

// Without an executor drain in between, a same-turn duplicate still returns
// the schema (the snapshot only updates between turns) — documented tradeoff.
func TestFetchTools_SameTurnDuplicateStillReturnsSchema(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
			"mail_archive": {Name: "mail_archive", Description: "Read local mail archive", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)

	da := toolport.NewDeferredActivation()
	ctx := toolport.WithDeferredActivation(context.Background(), da)

	if _, err := fn(ctx, mustJSON(t, map[string]any{"names": []string{"mail_archive"}})); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, err := fn(ctx, mustJSON(t, map[string]any{"names": []string{"mail_archive"}}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertActivated(t, out, "mail_archive")
}

func TestFetchTools_PresetRejectsDisallowedQueryResults(t *testing.T) {
	reg := &fakeFetchRegistry{
		defs: map[string]toolport.ToolDef{
			"cron": {Name: "cron", Description: "Schedule recurring jobs", Deferred: true},
		},
	}
	fn := ToolFetchTools(reg)
	ctx := toolport.WithToolPreset(context.Background(), "researcher")

	out, err := fn(ctx, mustJSON(t, map[string]any{"query": "schedule recurring"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No deferred tools match") {
		t.Fatalf("expected disallowed tool hidden from query results, got: %s", out)
	}
}
