package embedindex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A saved cache must round-trip through the manifest+blob split, and the
// manifest must no longer carry the numbers — that split is the whole point
// (mailstore's manifest was 89MB of decimal text).
func TestSaveCacheWritesBlobAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	ix := New("test", nil, path)
	ix.mu.Lock()
	ix.cacheDimensions = 4
	ix.vecs["a"] = cachedVec{hash: "h1", vec: []float32{1, 0, 0, 0}}
	ix.vecs["b"] = cachedVec{hash: "h2", vec: []float32{0, 1, 0, 0}}
	ix.mu.Unlock()
	ix.saveCache()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	var env cacheEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}
	if env.Version != cacheSchemaVersion || env.Dimensions != 4 {
		t.Fatalf("envelope = %+v", env)
	}
	for id, e := range env.Entries {
		if len(e.Vec) != 0 {
			t.Errorf("%s still carries inline numbers", id)
		}
		if e.Idx == nil {
			t.Errorf("%s has no blob index", id)
		}
	}
	blob, err := os.Stat(blobPathFor(path))
	if err != nil {
		t.Fatalf("blob: %v", err)
	}
	if blob.Size() != int64(2*4*4) {
		t.Errorf("blob is %d bytes, want %d", blob.Size(), 2*4*4)
	}

	reloaded := New("test", nil, path)
	reloaded.mu.Lock()
	defer reloaded.mu.Unlock()
	if got := reloaded.vecs["b"]; got.hash != "h2" || len(got.vec) != 4 || got.vec[1] != 1 {
		t.Fatalf("reloaded b = %+v", got)
	}
}

// A v2 manifest (vectors inline) still loads — the cache is expensive to rebuild
// and every live index is on v2 at the moment of the upgrade.
func TestLoadCacheReadsLegacyInlineVectors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.json")
	raw, err := json.Marshal(cacheEnvelope{
		Version:    2,
		Dimensions: 3,
		Entries: map[string]cachedVecWire{
			"a": {Hash: "h1", Vec: []float32{1, 2, 3}},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ix := New("test", nil, path)
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if got := ix.vecs["a"]; len(got.vec) != 3 || got.vec[2] != 3 {
		t.Fatalf("legacy entry = %+v", got)
	}
}

// A v3 manifest whose blob is missing or the wrong size must re-embed from
// scratch rather than slice the blob at offsets that mean nothing — that would
// hand every item someone else's embedding.
func TestLoadCacheDropsEverythingWhenBlobIsUnusable(t *testing.T) {
	for name, corrupt := range map[string]func(blobPath string){
		"missing": func(blobPath string) { os.Remove(blobPath) },
		"truncated": func(blobPath string) {
			data, _ := os.ReadFile(blobPath)
			_ = os.WriteFile(blobPath, data[:len(data)-3], 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "cache.json")
			ix := New("test", nil, path)
			ix.mu.Lock()
			ix.cacheDimensions = 4
			ix.vecs["a"] = cachedVec{hash: "h1", vec: []float32{1, 0, 0, 0}}
			ix.mu.Unlock()
			ix.saveCache()

			corrupt(blobPathFor(path))

			reloaded := New("test", nil, path)
			reloaded.mu.Lock()
			defer reloaded.mu.Unlock()
			if len(reloaded.vecs) != 0 {
				t.Fatalf("loaded %d vectors from an unusable blob", len(reloaded.vecs))
			}
		})
	}
}
