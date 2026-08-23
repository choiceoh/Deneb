package wiki

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSemanticCache_V4RoundTripAndLegacyMigration: vectors used to be JSON
// number arrays, so the live 2048-dimension cache was a 181MB file the gateway
// parsed synchronously on every boot (1.39s, ~335MB transient heap) and
// rewrote whenever any page changed. v4 keeps the manifest readable and moves
// the numbers into a float32 blob; a v3 file must still load and be rewritten.
func TestSemanticCache_V4RoundTripAndLegacyMigration(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	mustWrite(t, store, "프로젝트/risk.md", &Page{
		Meta: Frontmatter{ID: "risk", Title: "위험 평가", Category: "프로젝트"},
		Body: "납기 차질 우려가 있습니다. gpu 무관.",
	})
	store.SetEmbedder(fakeEmbedder{healthy: true, fingerprint: "model-a:2", dimensions: 2})
	if err := store.WarmSemanticIndex(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}

	manifestPath := filepath.Join(dir, "wiki", semanticCacheFile)
	blobPath := filepath.Join(dir, "wiki", semanticBlobFile)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var env semanticCacheEnvelopeV4
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("manifest is not v4 JSON: %v", err)
	}
	if env.Version != semanticCacheSchemaVersion || env.BlobVectors == 0 || len(env.Entries) == 0 {
		t.Fatalf("unexpected manifest: %+v", env)
	}
	// The numbers left the manifest.
	if len(raw) > 4096 {
		t.Errorf("manifest still carries vector payload (%d bytes)", len(raw))
	}
	blob, err := os.Stat(blobPath)
	if err != nil {
		t.Fatalf("blob missing: %v", err)
	}
	if want := int64(env.BlobVectors * env.Dimensions * 4); blob.Size() != want {
		t.Errorf("blob size = %d, want %d", blob.Size(), want)
	}

	// Restart: vectors come back without re-embedding, and search still works.
	restarted, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore restart: %v", err)
	}
	defer restarted.Close()
	counting := &countingEmbedder{fakeEmbedder: fakeEmbedder{healthy: true, fingerprint: "model-a:2", dimensions: 2}}
	restarted.SetEmbedder(counting)
	if status := restarted.SemanticStatus(); !status.Healthy || status.Pending != 0 {
		t.Fatalf("v4 cache did not hydrate: %+v", status)
	}
	if hits := restarted.searchSemanticWithVec(context.Background(), []float32{1, 0}, 5); len(hits) == 0 {
		t.Error("hydrated cache returns no semantic hits")
	}
	if counting.calls != 0 {
		t.Errorf("re-embedded %d batches despite a valid cache", counting.calls)
	}

	// A truncated blob must drop the cache instead of mis-slicing vectors.
	if err := os.WriteFile(blobPath, []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatalf("corrupt blob: %v", err)
	}
	corrupt, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore corrupt: %v", err)
	}
	defer corrupt.Close()
	corrupt.SetEmbedder(fakeEmbedder{healthy: true, fingerprint: "model-a:2", dimensions: 2})
	if status := corrupt.SemanticStatus(); status.Indexed != 0 {
		t.Errorf("corrupt blob hydrated %d entries", status.Indexed)
	}
}

// TestSemanticCache_ReadsLegacyV3Manifest: an existing wiki carries a v3 file;
// it must load (no forced re-embed of the whole corpus) and the next save
// rewrites it in the compact form.
func TestSemanticCache_ReadsLegacyV3Manifest(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := semanticCacheEnvelope{
		Version: 3, Fingerprint: "model-a:2", Dimensions: 2,
		Preprocessing: semanticPreprocessingVersion,
		Entries: map[string]cachedVecWire{
			"프로젝트/risk.md": {Hash: "h1", Vec: []float32{1, 0}},
		},
	}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(filepath.Join(wikiDir, semanticCacheFile), data, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(wikiDir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	store.SetEmbedder(fakeEmbedder{healthy: true, fingerprint: "model-a:2", dimensions: 2})

	if hits := store.searchSemanticWithVec(context.Background(), []float32{1, 0}, 5); len(hits) == 0 {
		t.Error("legacy v3 cache did not hydrate")
	}
}
