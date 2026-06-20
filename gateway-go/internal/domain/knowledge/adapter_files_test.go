package knowledge

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

// fixedEmbedder maps an exact text to a fixed vector (unknown text → zero
// vector, cosine 0). Mirrors the filestore test embedder so the adapter test can
// hand-place chunk cosines above/below the index's 0.73 floor deterministically.
type fixedEmbedder struct {
	vecs      map[string][]float32
	unhealthy bool
}

func (f *fixedEmbedder) IsHealthy() bool { return !f.unhealthy }
func (f *fixedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, ok := f.vecs[t]
		if !ok {
			v = []float32{0, 0, 0}
		}
		out[i] = v
	}
	return out, nil
}

// plainText is the trivial extractor (file bytes are already searchable text).
func plainText(_ context.Context, data []byte, _ string) string { return string(data) }

// newFilesAdapterFixture builds a real local store + semantic index with two
// files: a relevant one (body cosine 1.0 to the query, above floor) and an
// unrelated one (orthogonal, cosine 0, below floor). Returns the adapter and the
// query string.
func newFilesAdapterFixture(t *testing.T) (Adapter, *filestore.SemanticIndex, *fixedEmbedder) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	store, err := filestore.NewLocalStore(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	const relevantBody = "개발행위허가 신청서 제출 서류 안내 및 절차 설명입니다"
	const otherBody = "점심 메뉴 커피 음료 주문 목록 정리한 내용입니다"
	if _, err := store.Put(ctx, "/허가/개발행위허가 신청서.txt", []byte(relevantBody), true); err != nil {
		t.Fatalf("Put relevant: %v", err)
	}
	if _, err := store.Put(ctx, "/회의/점심.txt", []byte(otherBody), true); err != nil {
		t.Fatalf("Put other: %v", err)
	}

	const query = "개발행위허가 신청서 핵심 알려줘"
	embed := &fixedEmbedder{vecs: map[string][]float32{
		relevantBody: {1, 0, 0},
		otherBody:    {0, 1, 0},
		query:        {1, 0, 0}, // cos to relevant = 1.0 (>floor); to other = 0 (<floor)
	}}

	idx := filestore.NewSemanticIndex(filepath.Join(dir, "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	readFile := func(_ context.Context, path string) ([]byte, string, error) {
		data, ent, gerr := store.Get(context.Background(), path)
		if gerr != nil {
			return nil, "", gerr
		}
		name := path
		if ent != nil {
			name = ent.Name
		}
		return data, name, nil
	}

	ad := NewFilesAdapter(FilesAdapterDeps{Index: idx, Embed: embed, ExtractFn: plainText, ReadFile: readFile})
	if ad == nil {
		t.Fatal("NewFilesAdapter returned nil with a live index + embedder")
	}
	return ad, idx, embed
}

func TestFilesAdapter_Recall_MapsHitsAndRespectsFloor(t *testing.T) {
	ad, _, _ := newFilesAdapterFixture(t)

	hits, err := ad.Recall(context.Background(), "개발행위허가 신청서 핵심 알려줘", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1 (floor must reject the unrelated file): %+v", len(hits), hits)
	}
	h := hits[0]
	if h.Ref.Layer != LayerFiles {
		t.Errorf("ref layer = %q, want %q", h.Ref.Layer, LayerFiles)
	}
	if h.Ref.ID != "/허가/개발행위허가 신청서.txt" {
		t.Errorf("ref id = %q, want the relevant file path", h.Ref.ID)
	}
	if h.Ref.String() != "f:/허가/개발행위허가 신청서.txt" {
		t.Errorf("ref string = %q, want f:<path>", h.Ref.String())
	}
	if strings.TrimSpace(h.Snippet) == "" {
		t.Error("snippet should carry the matched chunk")
	}
	if h.Score < 0.73 {
		t.Errorf("score = %v, want >= floor 0.73 (cosine surfaced)", h.Score)
	}
}

func TestFilesAdapter_Recall_OffTopicEmpty(t *testing.T) {
	ad, _, _ := newFilesAdapterFixture(t)
	// A query with no vector mapping → zero query vector → cosine 0 to every file,
	// below the floor, and no lexical token overlap → zero hits.
	hits, err := ad.Recall(context.Background(), "전혀 무관한 우주 항공 주제 질문", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("off-topic query returned %d hits, want 0 (floor + no lexical overlap): %+v", len(hits), hits)
	}
}

func TestFilesAdapter_Recall_DegradesOnUnhealthyEmbedder(t *testing.T) {
	ad, _, embed := newFilesAdapterFixture(t)
	embed.unhealthy = true // simulate the embedding server going down mid-session
	hits, err := ad.Recall(context.Background(), "개발행위허가 신청서 핵심", 5)
	if err != nil {
		t.Fatalf("Recall should degrade to empty, not error: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("unhealthy embedder should yield 0 hits, got %d", len(hits))
	}
}

func TestFilesAdapter_Read_ReturnsFullText(t *testing.T) {
	ad, _, _ := newFilesAdapterFixture(t)
	doc, err := ad.Read(context.Background(), "/허가/개발행위허가 신청서.txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if doc.Ref.Layer != LayerFiles {
		t.Errorf("doc ref layer = %q, want %q", doc.Ref.Layer, LayerFiles)
	}
	if !strings.Contains(doc.Content, "개발행위허가") {
		t.Errorf("doc content missing extracted text: %q", doc.Content)
	}
	if doc.Meta["path"] != "/허가/개발행위허가 신청서.txt" {
		t.Errorf("doc meta path = %q", doc.Meta["path"])
	}
}

func TestFilesAdapter_Read_NoReaderErrors(t *testing.T) {
	ctx := context.Background()
	idx := filestore.NewSemanticIndex("")
	embed := &fixedEmbedder{}
	ad := NewFilesAdapter(FilesAdapterDeps{Index: idx, Embed: embed, ExtractFn: plainText}) // no ReadFile
	if ad == nil {
		t.Fatal("adapter should construct with index+embed even without a reader")
	}
	if _, err := ad.Read(ctx, "/x.txt"); err == nil {
		t.Error("Read without a wired reader should error")
	}
}

func TestNewFilesAdapter_NilWhenDegraded(t *testing.T) {
	if NewFilesAdapter(FilesAdapterDeps{Index: nil, Embed: &fixedEmbedder{}}) != nil {
		t.Error("nil index should yield nil adapter")
	}
	if NewFilesAdapter(FilesAdapterDeps{Index: filestore.NewSemanticIndex(""), Embed: nil}) != nil {
		t.Error("nil embedder should yield nil adapter")
	}
}
