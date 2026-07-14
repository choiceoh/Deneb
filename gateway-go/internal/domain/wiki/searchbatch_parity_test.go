package wiki

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestSearchBatchParity_ReturnsPerQuerySearchResults: SearchBatch(queries) must
// equal Search(q) per query (same BM25/blend/validity; only the embed is
// shared). Runs without an embedder (pure BM25 path) so the parity holds
// deterministically.
func TestSearchBatchParity_ReturnsPerQuerySearchResults(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	t.Cleanup(func() { store.Close() })

	write := func(path, title, summary, body string) {
		t.Helper()
		page := NewPage(title, "프로젝트", nil)
		page.Meta.Summary = summary
		page.Body = body
		if err := store.WritePage(path, page); err != nil {
			t.Fatalf("WritePage(%s): %v", path, err)
		}
	}
	write("금호타이어-곡성.md", "금호타이어 곡성 1단계", "곡성 공장 태양광 납기", "곡성 공정률 납기 지연 검토")
	write("당진-케이블.md", "당진 해저케이블", "당진 케이블 포설", "해저케이블 공정 지연")
	write("기아-화성.md", "기아 화성 국유지", "국유지 인허가", "인허가 진행 상황")

	queries := []string{"곡성 납기 지연", "해저케이블 공정", "국유지 인허가"}
	batch, err := store.SearchBatch(context.Background(), queries, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) != len(queries) {
		t.Fatalf("batch len %d != %d", len(batch), len(queries))
	}
	for i, q := range queries {
		want, err := store.Search(context.Background(), q, 3)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(batch[i], want) {
			t.Errorf("query %q: SearchBatch=%v Search=%v", q, batch[i], want)
		}
	}
}
