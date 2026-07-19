package embedindex

import (
	"context"
	"reflect"
	"testing"
)

type roleRecordingEmbedder struct {
	calls []string
}

func (e *roleRecordingEmbedder) IsHealthy() bool { return true }

func (e *roleRecordingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, "passage")
	return roleTestVectors(texts), nil
}

func (e *roleRecordingEmbedder) EmbedKind(_ context.Context, kind string, texts []string) ([][]float32, error) {
	e.calls = append(e.calls, kind)
	return roleTestVectors(texts), nil
}

func roleTestVectors(texts []string) [][]float32 {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, float32(i + 1)}
	}
	return out
}

func TestIndexUsesPassageRoleForCorpusAndQueryRoleForSearches(t *testing.T) {
	embedder := &roleRecordingEmbedder{}
	ix := New("roles", embedder, "")
	defer ix.Close()
	if err := ix.Warm(context.Background(), func() []Item {
		return []Item{{ID: "a", Hash: "a", Text: "corpus passage text"}}
	}); err != nil {
		t.Fatalf("Warm: %v", err)
	}

	_ = ix.Search(context.Background(), "semantic query text", 1)
	_ = ix.SearchBatch(context.Background(), []string{"first batch query", "second batch query"}, 1)

	if want := []string{"passage", "query", "query"}; !reflect.DeepEqual(embedder.calls, want) {
		t.Fatalf("embedding roles = %v, want %v", embedder.calls, want)
	}
}

func TestEmbedQueriesFallsBackToPlainEmbed(t *testing.T) {
	embedder := &recordingEmbedder{healthy: true}
	if _, err := EmbedQueries(context.Background(), embedder, []string{"fallback query"}); err != nil {
		t.Fatalf("EmbedQueries: %v", err)
	}
	if got := embedder.snapshotCalls(); !reflect.DeepEqual(got, [][]string{{"fallback query"}}) {
		t.Fatalf("fallback calls = %v", got)
	}
}
