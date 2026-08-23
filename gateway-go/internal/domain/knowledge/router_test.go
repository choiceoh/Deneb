package knowledge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
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

type mockFactWriter struct {
	mockAdapter
	recordedFact FactRecordOptions
	forgotten    FactForgetOptions
	factSubject  string
	factKey      string
	mutation     FactMutationResult
	factViews    []FactView
	factErr      error
}

type panicAdapter struct{ mockAdapter }

type mutatingRecallGuardAdapter struct {
	Adapter
	guard      RecallResultGuard
	afterFirst func()
	calls      int
}

func (a *mutatingRecallGuardAdapter) FilterRecallResults(query string, results []Result) []Result {
	a.calls++
	filtered := a.guard.FilterRecallResults(query, results)
	if a.calls == 1 && a.afterFirst != nil {
		a.afterFirst()
	}
	return filtered
}

func (p *panicAdapter) Recall(context.Context, string, int) ([]Result, error) {
	panic("broken connector")
}

func (m *mockWriter) Record(_ context.Context, opts RecordOptions) (Ref, error) {
	m.recorded = opts
	return m.out, m.wErr
}

func (m *mockFactWriter) RecordFact(_ context.Context, opts FactRecordOptions) (FactMutationResult, error) {
	m.recordedFact = opts
	return m.mutation, m.factErr
}

func (m *mockFactWriter) ForgetFact(_ context.Context, opts FactForgetOptions) (FactMutationResult, error) {
	m.forgotten = opts
	return m.mutation, m.factErr
}

func (m *mockFactWriter) Facts(_ context.Context, subject, key string) ([]FactView, error) {
	m.factSubject, m.factKey = subject, key
	return m.factViews, m.factErr
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

func TestRefStringFormatsLayerAndID(t *testing.T) {
	if got := (Ref{Layer: LayerWiki, ID: "인물/박부장"}).String(); got != "w:인물/박부장" {
		t.Errorf("String = %q, want %q", got, "w:인물/박부장")
	}
	if got := (Ref{}).String(); got != "" {
		t.Errorf("empty Ref.String = %q, want empty", got)
	}
}

func TestRouter_Recall_MergesAndReturnsHighestFirst(t *testing.T) {
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
	// Cross-layer RRF: both layer rank-1s outrank the layer rank-2. Wiki wins
	// the rank-1 tie (curated preference).
	if got[0].Ref.ID != "인물/박부장" {
		t.Errorf("first hit = %s, want wiki rank-1", got[0].Ref.String())
	}
	if got[1].Ref.ID != "mem-2" {
		t.Errorf("second hit = %s, want test-layer rank-1", got[1].Ref.String())
	}
	if got[2].Ref.ID != "mem-1" {
		t.Errorf("third hit = %s, want test-layer rank-2", got[2].Ref.String())
	}
}

func TestRouterRecallAppliesCanonicalFactGuardAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	for index, value := range []string{"8120만원", "9350만원"} {
		if _, err := store.UpsertFact(wiki.FactInput{
			Subject: "project:alpha", Key: "quote.amount", Value: value,
			Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
			Sources: []string{"doc:alpha"}, BasisAt: base.Add(time.Duration(index) * time.Hour),
			At: base.Add(time.Duration(index) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	files := &mockAdapter{layer: LayerFiles, results: []Result{
		{Ref: Ref{Layer: LayerFiles, ID: "/old-contract.pdf"}, Snippet: "alpha 견적 금액 8120만원", Context: "정정 전 계약", Score: 0.99},
	}}
	packet := New(NewWikiAdapter(store), files).RecallPacket(context.Background(), "alpha 견적 금액", 10, RecallOptions{})
	joined := ""
	for _, hit := range packet.Results {
		joined += "\n" + hit.Snippet + "\n" + hit.Context
		if hit.Ref.ID == "/old-contract.pdf" {
			t.Fatalf("files result bypassed canonical fact guard: %+v", hit)
		}
	}
	if strings.Contains(joined, "8120만원") || !strings.Contains(joined, "9350만원") {
		t.Fatalf("federated lifecycle result violated current-only contract:\n%s", joined)
	}
	if _, err := store.TombstoneFact(wiki.FactTombstoneInput{
		Subject: "project:alpha", Key: "quote.amount", Authority: wiki.FactAuthorityDirectUser,
		Reason: "cancelled", At: base.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	files.results = []Result{
		{Ref: Ref{Layer: LayerFiles, ID: "/old-contract.pdf"}, Snippet: "alpha 견적 금액 8120만원", Score: 0.99},
		{Ref: Ref{Layer: LayerFiles, ID: "/new-contract.pdf"}, Snippet: "alpha 견적 금액 9350만원", Score: 0.98},
	}
	packet = New(NewWikiAdapter(store), files).RecallPacket(context.Background(), "alpha 견적 금액", 10, RecallOptions{})
	for _, hit := range packet.Results {
		if strings.Contains(hit.Snippet+"\n"+hit.Context, "8120만원") || strings.Contains(hit.Snippet+"\n"+hit.Context, "9350만원") {
			t.Fatalf("tombstoned value re-entered federated recall: %+v", hit)
		}
	}
}

func TestRouterRecallRevalidatesCanonicalFactsAtExposureBoundary(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	if _, err := store.UpsertFact(wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: "8120만원",
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, BasisAt: base, At: base,
	}); err != nil {
		t.Fatal(err)
	}

	baseAdapter := NewWikiAdapter(store)
	guarded := &mutatingRecallGuardAdapter{
		Adapter: baseAdapter,
		guard:   baseAdapter.(RecallResultGuard),
		afterFirst: func() {
			if _, err := store.UpsertFact(wiki.FactInput{
				Subject: "project:alpha", Key: "quote.amount", Value: "9350만원",
				Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
				Sources: []string{"doc:alpha"}, BasisAt: base.Add(time.Hour), At: base.Add(time.Hour),
			}); err != nil {
				t.Fatalf("concurrent correction: %v", err)
			}
		},
	}
	files := &mockAdapter{layer: LayerFiles, results: []Result{{
		Ref:     Ref{Layer: LayerFiles, ID: "/old-contract.pdf"},
		Snippet: "alpha 견적 금액 8120만원", Score: 0.99,
	}}}

	packet := New(guarded, files).RecallPacket(context.Background(), "alpha 견적 금액", 10, RecallOptions{})
	if guarded.calls != 2 {
		t.Fatalf("lifecycle guard calls=%d, want pre-fusion plus exposure-boundary revalidation", guarded.calls)
	}
	for _, hit := range packet.Results {
		if strings.Contains(hit.Snippet+"\n"+hit.Context, "8120만원") || hit.Meta["factPlane"] == "current" {
			t.Fatalf("pre-correction evidence escaped the final guard: %+v", hit)
		}
	}
}

func TestRouterFactGuardScopesSharedShortValuesAcrossFilesAndRestart(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store, err := wiki.NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	upsert := func(input wiki.FactInput) {
		t.Helper()
		if _, upsertErr := store.UpsertFact(input); upsertErr != nil {
			t.Fatal(upsertErr)
		}
	}
	upsert(wiki.FactInput{
		Subject: "project:alpha", Key: "quote.amount", Value: "A",
		Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
		Sources: []string{"doc:alpha"}, At: base, BasisAt: base,
	})
	for index, value := range []string{"A", "B"} {
		at := base.Add(time.Duration(index) * time.Hour)
		upsert(wiki.FactInput{
			Subject: "project:beta", Key: "quote.amount", Value: value,
			Kind: wiki.FactKindAmount, Authority: wiki.FactAuthorityPrimaryDoc,
			Sources: []string{"doc:beta"}, At: at, BasisAt: at,
		})
	}

	files := &mockAdapter{layer: LayerFiles, results: []Result{
		{Ref: Ref{Layer: LayerFiles, ID: "/project/alpha/quote.txt"}, Snippet: "project alpha quote amount A", Score: 0.99},
		{Ref: Ref{Layer: LayerFiles, ID: "/project/beta/quote-old.txt"}, Snippet: "project beta quote amount A", Score: 0.98},
		{Ref: Ref{Layer: LayerFiles, ID: "/project/beta/quote-current.txt"}, Snippet: "project beta quote amount B", Score: 0.97},
		{Ref: Ref{Layer: LayerFiles, ID: "/quality/grade.txt"}, Snippet: "quality grade A passes", Score: 0.96},
	}}
	assertFiles := func(label string, current *wiki.Store, tombstoned bool) {
		t.Helper()
		packet := New(NewWikiAdapter(current), files).RecallPacket(
			context.Background(), "project beta quote amount", 20, RecallOptions{},
		)
		seen := make(map[string]bool)
		for _, hit := range packet.Results {
			seen[hit.Ref.ID] = true
		}
		for _, required := range []string{"/project/alpha/quote.txt", "/quality/grade.txt"} {
			if !seen[required] {
				t.Fatalf("%s unrelated shared A result %q was removed: %+v", label, required, packet.Results)
			}
		}
		if seen["/project/beta/quote-old.txt"] {
			t.Fatalf("%s beta old A survived: %+v", label, packet.Results)
		}
		if seen["/project/beta/quote-current.txt"] == tombstoned {
			t.Fatalf("%s beta current presence=%v, tombstoned=%v: %+v", label, seen["/project/beta/quote-current.txt"], tombstoned, packet.Results)
		}
	}
	assertFiles("corrected", store, false)
	if _, err := store.TombstoneFact(wiki.FactTombstoneInput{
		Subject: "project:beta", Key: "quote.amount",
		Authority: wiki.FactAuthorityDirectUser, At: base.Add(2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	assertFiles("tombstoned", store, true)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := wiki.NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertFiles("reopened tombstone", reopened, true)
}

// TestRouter_Recall_RRFPrefersWikiRankOverFilesCosine ensures final ordering
// uses per-layer rank RRF, not raw backend scores: a low wiki BM25 rank-1 still
// beats a high files cosine rank-2.
func TestRouter_Recall_RRFPrefersWikiRankOverFilesCosine(t *testing.T) {
	wikiA := &mockAdapter{layer: LayerWiki, results: []Result{
		{Ref: Ref{Layer: LayerWiki, ID: "인물/박부장"}, Score: 0.40},
	}}
	files := &mockAdapter{layer: LayerFiles, results: []Result{
		{Ref: Ref{Layer: LayerFiles, ID: "/a.pdf"}, Score: 0.95},
		{Ref: Ref{Layer: LayerFiles, ID: "/b.pdf"}, Score: 0.90},
	}}
	r := New(wikiA, files)
	got := r.Recall(context.Background(), "q", 5)
	if len(got) != 3 {
		t.Fatalf("got %d hits, want 3", len(got))
	}
	if got[0].Ref.Layer != LayerWiki {
		t.Fatalf("first hit layer = %s, want wiki (rank-1 tie → curated)", got[0].Ref.Layer)
	}
	if got[1].Ref.ID != "/a.pdf" {
		t.Fatalf("second hit = %s, want files rank-1", got[1].Ref.String())
	}
	if got[2].Ref.ID != "/b.pdf" {
		t.Fatalf("third hit = %s, want files rank-2 (must not beat wiki via cosine)", got[2].Ref.String())
	}
}

func TestRouter_Recall_FileIntentUsesPlannerPriorityForCrossLayerTie(t *testing.T) {
	wikiA := &mockAdapter{layer: LayerWiki, results: []Result{
		{Ref: Ref{Layer: LayerWiki, ID: "설계/router"}, Score: 0.9},
	}}
	files := &mockAdapter{layer: LayerFiles, results: []Result{
		{Ref: Ref{Layer: LayerFiles, ID: "/src/router.go"}, Score: 0.8},
	}}
	packet := New(wikiA, files).RecallPacket(context.Background(), "router.go 함수 구현", 5, RecallOptions{})
	if len(packet.Results) != 2 || packet.Results[0].Ref.Layer != LayerFiles {
		t.Fatalf("file-intent results = %+v, want files rank-1 tie first", packet.Results)
	}
	if len(packet.Plan.Sources) != 2 || packet.Plan.Sources[0].Source.Layer != LayerFiles {
		t.Fatalf("file-intent plan = %+v", packet.Plan.Sources)
	}
}

func TestRouter_RecallWithMeta_FilesTimeoutNote(t *testing.T) {
	wikiA := &mockAdapter{layer: LayerWiki, results: []Result{
		{Ref: Ref{Layer: LayerWiki, ID: "p"}, Score: 0.7},
	}}
	files := &mockAdapter{layer: LayerFiles, recErr: ErrFilesRecallTimeout}
	r := New(wikiA, files)
	hits, notes := r.RecallWithMeta(context.Background(), "q", 5)
	if len(hits) != 1 || hits[0].Ref.Layer != LayerWiki {
		t.Fatalf("hits = %+v, want single wiki hit", hits)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "타임아웃") {
		t.Fatalf("notes = %v, want files timeout note", notes)
	}
}

// TestRouter_Recall_LayerQuotaPreservesOtherLayerHit pins the per-layer quota: a layer that returns
// many high-scoring hits must not sweep the whole merged window — the other
// layer's hit survives even though every files hit outscores it. This is the
// hindsight-lesson guard (a higher score band must not monopolize recall).
func TestRouter_Recall_LayerQuotaPreservesOtherLayerHit(t *testing.T) {
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

// TestRouter_Recall_SingleLayerReturnsAllHits ensures the quota never throttles a
// single-layer router (the production wiki-only case before files wiring).
func TestRouter_Recall_SingleLayerReturnsAllHits(t *testing.T) {
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

func TestRouter_Recall_EmptySelectedLayerDoesNotThrottleOnlyContributor(t *testing.T) {
	wikiA := &mockAdapter{layer: LayerWiki}
	for i := 0; i < 5; i++ {
		wikiA.results = append(wikiA.results, Result{
			Ref: Ref{Layer: LayerWiki, ID: "p" + string(rune('A'+i))}, Score: 0.9 - float64(i)*0.1,
		})
	}
	files := &mockAdapter{layer: LayerFiles}
	got := New(wikiA, files).Recall(context.Background(), "q", 5)
	if len(got) != 5 {
		t.Fatalf("empty sibling layer throttled the only contributor: got %d, want 5", len(got))
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

func TestRouter_Recall_SourcePanicDegradesWithoutCrashingFanout(t *testing.T) {
	good := &mockAdapter{layer: LayerWiki, results: []Result{{Ref: Ref{Layer: LayerWiki, ID: "p"}, Score: 0.5}}}
	broken := &panicAdapter{mockAdapter{layer: LayerFiles}}
	packet := New(good, broken).RecallPacket(context.Background(), "x", 5, RecallOptions{})
	if len(packet.Results) != 1 || packet.Results[0].Ref.Layer != LayerWiki {
		t.Fatalf("results = %+v, want surviving wiki hit", packet.Results)
	}
	if len(packet.Notes) != 1 || !strings.Contains(packet.Notes[0], "제외") {
		t.Fatalf("notes = %v, want source degrade note", packet.Notes)
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

func TestRouterFactAPISelectsAndDispatchesToFactAdapter(t *testing.T) {
	f := &mockFactWriter{
		mockAdapter: mockAdapter{layer: LayerWiki},
		mutation:    FactMutationResult{Revision: 7, ClaimID: "fact-7", Committed: true},
		factViews:   []FactView{{ID: "fact-7", Subject: "self", Key: "answer.length"}},
	}
	r := New(&mockAdapter{layer: layerTest}, f)

	created, err := r.RecordFact(context.Background(), FactRecordOptions{Key: "answer.length", Value: "short"})
	if err != nil || created.ClaimID != "fact-7" || f.recordedFact.Value != "short" {
		t.Fatalf("RecordFact = %+v, err = %v, captured = %+v", created, err, f.recordedFact)
	}
	forgotten, err := r.ForgetFact(context.Background(), FactForgetOptions{Key: "answer.length", Reason: "requested"})
	if err != nil || forgotten.Revision != 7 || f.forgotten.Reason != "requested" {
		t.Fatalf("ForgetFact = %+v, err = %v, captured = %+v", forgotten, err, f.forgotten)
	}
	views, err := r.Facts(context.Background(), "self", "answer.length")
	if err != nil || len(views) != 1 || f.factSubject != "self" || f.factKey != "answer.length" {
		t.Fatalf("Facts = %+v, err = %v, captured = %q/%q", views, err, f.factSubject, f.factKey)
	}
}

func TestRouterFactAPIRequiresFactAdapter(t *testing.T) {
	for name, call := range map[string]func(*Router) error{
		"record": func(r *Router) error {
			_, err := r.RecordFact(context.Background(), FactRecordOptions{Key: "x", Value: "y"})
			return err
		},
		"forget": func(r *Router) error {
			_, err := r.ForgetFact(context.Background(), FactForgetOptions{Key: "x"})
			return err
		},
		"facts": func(r *Router) error {
			_, err := r.Facts(context.Background(), "self", "")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(New(&mockAdapter{layer: LayerWiki})); err == nil {
				t.Fatal("expected unavailable fact adapter error")
			}
			var nilRouter *Router
			if err := call(nilRouter); err == nil {
				t.Fatal("expected nil router error")
			}
		})
	}
}

func TestRouterFactMutationObserverRunsOnlyAfterCommit(t *testing.T) {
	f := &mockFactWriter{
		mockAdapter: mockAdapter{layer: LayerWiki},
		mutation:    FactMutationResult{Revision: 11, ClaimID: "fact-11", Committed: true, ProjectionError: "stale projection"},
	}
	r := New(f)
	var observed []FactMutationResult
	r.SetFactMutationObserver(func(result FactMutationResult) {
		observed = append(observed, result)
	})
	if _, err := r.RecordFact(context.Background(), FactRecordOptions{Key: "x", Value: "y"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ForgetFact(context.Background(), FactForgetOptions{Key: "x"}); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 || observed[0].Revision != 11 || observed[0].ProjectionError != "stale projection" {
		t.Fatalf("observed = %+v", observed)
	}

	f.mutation = FactMutationResult{Revision: 12, Committed: false}
	if _, err := r.RecordFact(context.Background(), FactRecordOptions{Key: "x", Value: "z"}); err != nil {
		t.Fatal(err)
	}
	if len(observed) != 2 {
		t.Fatalf("non-committed result notified observer: %+v", observed)
	}
	f.mutation = FactMutationResult{Revision: 13, Committed: true}
	f.factErr = errors.New("post-commit failure")
	if _, err := r.ForgetFact(context.Background(), FactForgetOptions{Key: "x"}); err == nil {
		t.Fatal("expected writer error")
	}
	if len(observed) != 3 || observed[2].Revision != 13 {
		t.Fatalf("committed result with error did not notify observer: %+v", observed)
	}
	f.mutation = FactMutationResult{Revision: 14, Committed: false}
	if _, err := r.ForgetFact(context.Background(), FactForgetOptions{Key: "x"}); err == nil {
		t.Fatal("expected writer error")
	}
	if len(observed) != 3 {
		t.Fatalf("uncommitted failed result notified observer: %+v", observed)
	}
}

func TestRouterFactMutationObserverPanicDoesNotReverseCommit(t *testing.T) {
	f := &mockFactWriter{
		mockAdapter: mockAdapter{layer: LayerWiki},
		mutation:    FactMutationResult{Revision: 13, ClaimID: "fact-13", Committed: true},
	}
	r := New(f)
	r.SetFactMutationObserver(func(FactMutationResult) { panic("cache invalidator failed") })
	result, err := r.RecordFact(context.Background(), FactRecordOptions{Key: "x", Value: "y"})
	if err != nil || !result.Committed || result.Revision != 13 {
		t.Fatalf("RecordFact = %+v, err = %v", result, err)
	}
	r.SetFactMutationObserver(nil)
}

func TestNew_IgnoresNil(t *testing.T) {
	r := New(nil, &mockAdapter{layer: LayerWiki}, nil)
	if len(r.Layers()) != 1 {
		t.Errorf("nil adapters should be skipped, got layers %v", r.Layers())
	}
}
