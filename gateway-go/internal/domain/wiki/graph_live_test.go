package wiki

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/embedding"
)

// TestGraphContextLive renders GraphContext against the real on-disk wiki, once
// token-only and once with the embedding rerank wired, so the "유사" neighbors
// the dense signal adds are visible. Skipped in CI; needs the BGE-M3 sidecar.
//
//	DENEB_WIKI_GRAPH_LIVE=1 DENEB_WIKI_GRAPH_Q=비금도 \
//	  go test -run TestGraphContextLive -v ./internal/domain/wiki/
func TestGraphContextLive(t *testing.T) {
	if os.Getenv("DENEB_WIKI_GRAPH_LIVE") == "" {
		t.Skip("set DENEB_WIKI_GRAPH_LIVE=1 to run against ~/.deneb/wiki")
	}
	home, _ := os.UserHomeDir()
	store, err := NewStore(filepath.Join(home, ".deneb", "wiki"), t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	q := os.Getenv("DENEB_WIKI_GRAPH_Q")
	if q == "" {
		q = "비금도"
	}
	ctx := context.Background()

	out, err := store.GraphContext(ctx, q, 10)
	if err != nil {
		t.Fatalf("GraphContext (token-only): %v", err)
	}
	t.Logf("──────────── token-only ────────────\n%s", out)

	emb := embedding.New("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < 50 && !emb.IsHealthy(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	store.SetEmbedder(emb)
	// The second call's applyEmbeddingRerank refreshes the index and folds in
	// cosine; the better-ordered neighbors (and a semantically-close page with
	// no explicit edge entering as "유사") are the win the benchmark predicted.
	out2, err := store.GraphContext(ctx, q, 10)
	if err != nil {
		t.Fatalf("GraphContext (with embedder): %v", err)
	}
	t.Logf("──────────── token + embedding rerank ────────────\n%s", out2)
}
