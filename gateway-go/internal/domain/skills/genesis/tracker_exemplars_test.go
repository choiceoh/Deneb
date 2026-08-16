package genesis

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type exemplarSemanticEmbedder struct {
	mu      sync.Mutex
	kinds   []string
	queries []string
}

func (e *exemplarSemanticEmbedder) IsHealthy() bool { return true }

func (e *exemplarSemanticEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.kinds = append(e.kinds, "passage")
	e.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if strings.Contains(text, "remote service stopped answering") {
			out[i] = []float32{1, 0}
		} else {
			out[i] = []float32{0, 1}
		}
	}
	return out, nil
}

func (e *exemplarSemanticEmbedder) EmbedKind(_ context.Context, kind string, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.kinds = append(e.kinds, kind)
	if kind == "query" {
		e.queries = append(e.queries, texts...)
	}
	e.mu.Unlock()
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if strings.Contains(text, "외부 연동 응답이 늦어") {
			out[i] = []float32{1, 0}
		} else {
			out[i] = []float32{0, 1}
		}
	}
	return out, nil
}

func (e *exemplarSemanticEmbedder) snapshotKinds() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.kinds...)
}

func (e *exemplarSemanticEmbedder) snapshotQueries() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.queries...)
}

func TestConfirmedEvolveExemplars_RetrievalContract(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	confirm := func(skill, sig string) {
		t.Helper()
		if err := tr.logEvolveConfirmed(skill, HarnessEditAudit{TargetSignature: sig, EditedSurface: "Procedure", ExpectedBehaviorChange: "회상 선행"}, true); err != nil {
			t.Fatal(err)
		}
	}
	confirm("skill-a", "wiki search returns empty")
	confirm("skill-b", "Wiki  Search Returns EMPTY") // 대소문자/공백 정규화 일치
	confirm("skill-c", "완전히 다른 실패")
	confirm("skill-self", "wiki search returns empty") // 자기 자신 — 제외 대상

	got, err := tr.confirmedEvolveExemplars([]string{"wiki search returns empty"}, "skill-self", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("exemplars = %d, want 2 (normalized cross-skill matches only): %+v", len(got), got)
	}
	for _, ex := range got {
		if ex.SkillName == "skill-self" || ex.SkillName == "skill-c" {
			t.Fatalf("retrieval leaked %q", ex.SkillName)
		}
		if ex.Audit.PrimaryDimension == "" {
			t.Fatalf("retrieved legacy-compatible exemplar lacks diagnosis: %+v", ex)
		}
	}
	// newest first
	if got[0].SkillName != "skill-b" {
		t.Fatalf("order not newest-first: %+v", got)
	}
	// limit + empty inputs
	if one, _ := tr.confirmedEvolveExemplars([]string{"wiki search returns empty"}, "", 1); len(one) != 1 {
		t.Fatalf("limit not applied: %+v", one)
	}
	if none, _ := tr.confirmedEvolveExemplars(nil, "", 3); none != nil {
		t.Fatalf("no signatures must return nil: %+v", none)
	}
}

func TestFormatConfirmedEvolveExemplars(t *testing.T) {
	if formatConfirmedEvolveExemplars(nil) != "" {
		t.Fatal("empty exemplars must render nothing")
	}
	out := formatConfirmedEvolveExemplars([]confirmedEvolveExemplar{{
		SkillName: "skill-a",
		Audit:     HarnessEditAudit{TargetSignature: "sig", EditedSurface: "Procedure", ExpectedBehaviorChange: "회상 선행"},
	}})
	for _, want := range []string{"검증 완주한 개선 사례", "[skill-a]", HarnessDimensionContextAssembly, HarnessDimensionOrchestration, "Procedure", "회상 선행"} {
		if !strings.Contains(out, want) {
			t.Fatalf("section missing %q:\n%s", want, out)
		}
	}
}

func TestEvolutionHealthReturnsFalseAcceptRateComplementingConfirmRate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("a", HarnessEditAudit{}, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("b", HarnessEditAudit{}, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveRolledBack("c"); err != nil {
		t.Fatal(err)
	}
	h := tr.EvolutionHealth()
	if h.ResolvedEvolves7d != 3 {
		t.Fatalf("resolved n = %d, want 3", h.ResolvedEvolves7d)
	}
	if h.FalseAcceptRate < 0.33 || h.FalseAcceptRate > 0.34 {
		t.Fatalf("falseAcceptRate = %v, want 1/3", h.FalseAcceptRate)
	}
	if diff := h.ConfirmRate + h.FalseAcceptRate; diff < 0.999 || diff > 1.001 {
		t.Fatalf("rates must complement: %v + %v", h.ConfirmRate, h.FalseAcceptRate)
	}
}

// Mechanism-level fallback (ToE 2606.06960): when no confirmed evolve matches
// the exact signature, one sharing the mechanism=… component still surfaces;
// unrelated mechanisms stay excluded.
func TestConfirmedEvolveExemplars_MechanismFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("sk-donor", HarnessEditAudit{
		TargetSignature: "terminal=timeout|mechanism=tool-plan-drift",
	}, true); err != nil {
		t.Fatal(err)
	}
	// Different terminal, same mechanism → only the fallback can find it.
	got, err := tr.confirmedEvolveExemplars([]string{"terminal=crash|mechanism=tool-plan-drift"}, "sk-target", 3)
	if err != nil || len(got) != 1 || got[0].SkillName != "sk-donor" {
		t.Fatalf("mechanism fallback missed the donor: %+v err=%v", got, err)
	}
	// No shared mechanism → nothing.
	if got, _ := tr.confirmedEvolveExemplars([]string{"terminal=crash|mechanism=other"}, "sk-target", 3); len(got) != 0 {
		t.Fatalf("unrelated mechanism must not match: %+v", got)
	}
}

func TestConfirmedEvolveExemplars_SemanticFallbackFindsAnalogousSuccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("remote-recovery", HarnessEditAudit{
		TargetSignature:        "remote service stopped answering during a tool call",
		EditedSurface:          "Procedure",
		ExpectedBehaviorChange: "retry with a bounded fallback",
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("formatting", HarnessEditAudit{
		TargetSignature:        "markdown table alignment mismatch",
		EditedSurface:          "Output",
		ExpectedBehaviorChange: "normalize cell widths",
	}, true); err != nil {
		t.Fatal(err)
	}
	embedder := &exemplarSemanticEmbedder{}
	tr.SetExemplarEmbedder(embedder)

	got, err := tr.confirmedEvolveExemplarsContext(
		context.Background(),
		[]string{"외부 연동 응답이 늦어 도구 실행이 멈춤"},
		"target-skill",
		3,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SkillName != "remote-recovery" {
		t.Fatalf("semantic exemplars = %+v", got)
	}
	if want := []string{"passage", "query"}; !reflect.DeepEqual(embedder.snapshotKinds(), want) {
		t.Fatalf("embedding roles = %v, want %v", embedder.snapshotKinds(), want)
	}
	if queries := embedder.snapshotQueries(); len(queries) != 1 || !strings.Contains(queries[0], HarnessDimensionContextAssembly) {
		t.Fatalf("semantic query lacks harness dimension: %+v", queries)
	}
}

func TestConfirmedEvolveExemplars_ExactMatchPrecedesSemanticLookup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.logEvolveConfirmed("exact", HarnessEditAudit{TargetSignature: "same failure"}, true); err != nil {
		t.Fatal(err)
	}
	embedder := &exemplarSemanticEmbedder{}
	tr.SetExemplarEmbedder(embedder)

	got, err := tr.confirmedEvolveExemplarsContext(context.Background(), []string{"same failure"}, "target", 1)
	if err != nil || len(got) != 1 || got[0].SkillName != "exact" {
		t.Fatalf("exact exemplars = %+v err=%v", got, err)
	}
	if calls := embedder.snapshotKinds(); len(calls) != 0 {
		t.Fatalf("exact match unnecessarily embedded: %v", calls)
	}
}

func TestEmbedConfirmedExemplarPassagesBatchesBeyondSidecarLimit(t *testing.T) {
	embedder := &exemplarSemanticEmbedder{}
	texts := make([]string, confirmedExemplarEmbedBatch*2+1)
	for i := range texts {
		texts[i] = "remote service stopped answering"
	}

	passages, ok := embedConfirmedExemplarPassages(context.Background(), embedder, texts)
	if !ok || len(passages) != len(texts) {
		t.Fatalf("batched passages = %d ok=%v, want %d", len(passages), ok, len(texts))
	}
	if want := []string{"passage", "passage", "passage"}; !reflect.DeepEqual(embedder.snapshotKinds(), want) {
		t.Fatalf("embedding batches = %v, want %v", embedder.snapshotKinds(), want)
	}
}
