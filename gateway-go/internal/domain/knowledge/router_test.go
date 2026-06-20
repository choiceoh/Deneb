package knowledge

import (
	"context"
	"errors"
	"testing"
)

// layerTest is a synthetic second layer for exercising the router's
// multi-backend merge/routing logic (wiki is the only production backend).
const layerTest Layer = "t"

// mockAdapter is a minimal Adapter for testing router behavior.
type mockAdapter struct {
	layer   Layer
	results []Result
	doc     *Document
	recErr  error
	readErr error
}

func (m *mockAdapter) Layer() Layer { return m.layer }
func (m *mockAdapter) Recall(_ context.Context, _ string, _ int) ([]Result, error) {
	return m.results, m.recErr
}
func (m *mockAdapter) Read(_ context.Context, _ string) (*Document, error) {
	return m.doc, m.readErr
}

// mockWriter adds Record on top.
type mockWriter struct {
	mockAdapter
	recorded RecordOptions
	out      Ref
	wErr     error
}

func (m *mockWriter) Record(_ context.Context, opts RecordOptions) (Ref, error) {
	m.recorded = opts
	return m.out, m.wErr
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		in    string
		want  Ref
		isErr bool
	}{
		{"w:인물/박부장", Ref{Layer: LayerWiki, ID: "인물/박부장"}, false},
		{"f:/메일/계약서.pdf", Ref{Layer: LayerFiles, ID: "/메일/계약서.pdf"}, false},
		{"h:mem-abc", Ref{}, true}, // retired layer → unknown
		{"  w:거래/ABC상사  ", Ref{Layer: LayerWiki, ID: "거래/ABC상사"}, false},
		{"", Ref{}, true},
		{":no-layer", Ref{}, true},
		{"w:", Ref{}, true},
		{"unknown:x", Ref{}, true},
		{"missing-colon", Ref{}, true},
	}
	for _, c := range cases {
		got, err := ParseRef(c.in)
		if c.isErr {
			if err == nil {
				t.Errorf("ParseRef(%q) expected error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRef(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseRef(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestRefString(t *testing.T) {
	if got := (Ref{Layer: LayerWiki, ID: "인물/박부장"}).String(); got != "w:인물/박부장" {
		t.Errorf("String = %q, want %q", got, "w:인물/박부장")
	}
	if got := (Ref{}).String(); got != "" {
		t.Errorf("empty Ref.String = %q, want empty", got)
	}
}

func TestRouter_Recall_Merges(t *testing.T) {
	wikiA := &mockAdapter{
		layer: LayerWiki,
		results: []Result{
			{Ref: Ref{Layer: LayerWiki, ID: "인물/박부장"}, Snippet: "wiki hit", Score: 0.9},
		},
	}
	hsA := &mockAdapter{
		layer: layerTest,
		results: []Result{
			{Ref: Ref{Layer: layerTest, ID: "mem-1"}, Snippet: "h hit 1", Score: 0.8},
			{Ref: Ref{Layer: layerTest, ID: "mem-2"}, Snippet: "h hit 2", Score: 0.95},
		},
	}
	r := New(wikiA, hsA)

	got := r.Recall(context.Background(), "박부장", 5)
	if len(got) != 3 {
		t.Fatalf("got %d hits, want 3", len(got))
	}
	// Highest score first.
	if got[0].Score != 0.95 {
		t.Errorf("first hit score = %v, want 0.95", got[0].Score)
	}
}

// TestRouter_Recall_LayerQuota pins the per-layer quota: a layer that returns
// many high-scoring hits must not sweep the whole merged window — the other
// layer's hit survives even though every files hit outscores it. This is the
// hindsight-lesson guard (a higher score band must not monopolize recall).
func TestRouter_Recall_LayerQuota(t *testing.T) {
	// Files layer: 5 hits, all scoring ABOVE the single wiki hit.
	files := &mockAdapter{layer: LayerFiles}
	for i := 0; i < 5; i++ {
		files.results = append(files.results, Result{
			Ref:   Ref{Layer: LayerFiles, ID: "/f" + string(rune('A'+i))},
			Score: 0.90 + float64(i)*0.01, // 0.90..0.94, all > wiki's 0.50
		})
	}
	wikiA := &mockAdapter{layer: LayerWiki, results: []Result{
		{Ref: Ref{Layer: LayerWiki, ID: "인물/박부장"}, Score: 0.50},
	}}
	r := New(wikiA, files)

	// limit 5 → quota = ceil(5*0.6) = 3 files max, leaving room for the wiki hit.
	got := r.Recall(context.Background(), "q", 5)
	var wikiSeen, fileCount int
	for _, h := range got {
		if h.Ref.Layer == LayerWiki {
			wikiSeen++
		}
		if h.Ref.Layer == LayerFiles {
			fileCount++
		}
	}
	if wikiSeen != 1 {
		t.Errorf("wiki hit was crowded out by the higher-band files layer (quota failed): got %+v", got)
	}
	if fileCount > 3 {
		t.Errorf("files layer exceeded its quota: %d files in %+v", fileCount, got)
	}
}

// TestRouter_Recall_SingleLayerUnquota'd ensures the quota never throttles a
// single-layer router (the production wiki-only case before files wiring).
func TestRouter_Recall_SingleLayerUnquotaed(t *testing.T) {
	wikiA := &mockAdapter{layer: LayerWiki}
	for i := 0; i < 5; i++ {
		wikiA.results = append(wikiA.results, Result{
			Ref:   Ref{Layer: LayerWiki, ID: "p" + string(rune('A'+i))},
			Score: 0.9 - float64(i)*0.1,
		})
	}
	r := New(wikiA)
	got := r.Recall(context.Background(), "q", 5)
	if len(got) != 5 {
		t.Fatalf("single-layer router should not be quota-throttled: got %d, want 5", len(got))
	}
}

func TestRouter_Recall_OneFails(t *testing.T) {
	good := &mockAdapter{
		layer: LayerWiki,
		results: []Result{
			{Ref: Ref{Layer: LayerWiki, ID: "p"}, Score: 0.5},
		},
	}
	bad := &mockAdapter{
		layer:  layerTest,
		recErr: errors.New("backend down"),
	}
	r := New(good, bad)
	got := r.Recall(context.Background(), "x", 5)
	if len(got) != 1 {
		t.Errorf("got %d hits, want 1 (bad adapter swallowed)", len(got))
	}
}

func TestRouter_Read_Routes(t *testing.T) {
	wd := &Document{Ref: Ref{Layer: LayerWiki, ID: "p"}, Content: "wiki body"}
	wikiA := &mockAdapter{layer: LayerWiki, doc: wd}
	hsA := &mockAdapter{layer: layerTest, readErr: errors.New("not supported")}
	r := New(wikiA, hsA)

	got, err := r.Read(context.Background(), Ref{Layer: LayerWiki, ID: "p"})
	if err != nil || got != wd {
		t.Errorf("wiki read = %+v, %v", got, err)
	}

	_, err = r.Read(context.Background(), Ref{Layer: layerTest, ID: "x"})
	if err == nil {
		t.Error("retired-layer read should error (mock returns error)")
	}
}

func TestRouter_Record_RequiresWriter(t *testing.T) {
	r := New(&mockAdapter{layer: layerTest}) // no writer
	_, err := r.Record(context.Background(), RecordOptions{Page: "x", Body: "y"})
	if err == nil {
		t.Error("expected error when no writable adapter is registered")
	}
}

func TestRouter_Record_DispatchesToWriter(t *testing.T) {
	w := &mockWriter{
		mockAdapter: mockAdapter{layer: LayerWiki},
		out:         Ref{Layer: LayerWiki, ID: "인물/박부장"},
	}
	r := New(w, &mockAdapter{layer: layerTest})
	got, err := r.Record(context.Background(), RecordOptions{Page: "인물/박부장", Body: "..."})
	if err != nil {
		t.Fatalf("Record err: %v", err)
	}
	if got.String() != "w:인물/박부장" {
		t.Errorf("ref = %q, want w:인물/박부장", got.String())
	}
	if w.recorded.Page != "인물/박부장" {
		t.Errorf("writer not called with page; got %+v", w.recorded)
	}
}

func TestNew_IgnoresNil(t *testing.T) {
	r := New(nil, &mockAdapter{layer: LayerWiki}, nil)
	if len(r.Layers()) != 1 {
		t.Errorf("nil adapters should be skipped, got layers %v", r.Layers())
	}
}
