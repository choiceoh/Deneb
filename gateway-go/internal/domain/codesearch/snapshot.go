package codesearch

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type indexStamp struct {
	metaSize int64
	metaTime int64
	vecSize  int64
	vecTime  int64
}

type indexSnapshot struct {
	stamp indexStamp
	meta  Meta
	vecs  [][]float32
	norms []float64
}

var semanticSnapshots = struct {
	sync.RWMutex
	byDir map[string]*indexSnapshot
}{byDir: make(map[string]*indexSnapshot)}

// loadIndexSnapshot keeps the large JSON/vector sidecars hot across tool calls.
// Search used to parse metadata and read 100+ MiB of vectors on every query;
// the snapshot turns repeat latency into one query embedding plus an in-memory
// scan. Stamps preserve freshness, and all disk I/O happens outside the lock.
func loadIndexSnapshot(dir string) (*indexSnapshot, error) {
	dir = filepath.Clean(dir)
	stamp, err := statIndex(dir)
	if err != nil {
		return nil, err
	}
	semanticSnapshots.RLock()
	cached := semanticSnapshots.byDir[dir]
	semanticSnapshots.RUnlock()
	if cached != nil && cached.stamp == stamp {
		return cached, nil
	}

	for attempt := 0; attempt < 2; attempt++ {
		meta, ok := LoadMeta(dir)
		if !ok {
			return nil, fmt.Errorf("semantic index missing or stale — run `codesearch index` first")
		}
		vecs, err := loadVectors(dir, meta)
		if err != nil {
			after, statErr := statIndex(dir)
			if statErr == nil && after != stamp {
				stamp = after
				continue
			}
			// saveIndex cuts over vectors before metadata. A reader landing in
			// that tiny window sees a stable-but-mismatched pair until the second
			// rename; yield once instead of surfacing a transient rebuild error.
			if attempt == 0 {
				time.Sleep(15 * time.Millisecond)
				if refreshed, statErr := statIndex(dir); statErr == nil {
					stamp = refreshed
					continue
				}
			}
			return nil, err
		}
		after, err := statIndex(dir)
		if err != nil {
			return nil, err
		}
		if after != stamp {
			stamp = after
			continue // writer cut over between the two sidecar reads
		}
		norms := make([]float64, len(vecs))
		for i, vector := range vecs {
			norms[i] = vectorNorm(vector)
		}
		loaded := &indexSnapshot{stamp: stamp, meta: meta, vecs: vecs, norms: norms}
		semanticSnapshots.Lock()
		// Multiple developer worktrees can call Search in one test/process. Keep
		// at most two heavyweight snapshots rather than leaking one per path.
		if len(semanticSnapshots.byDir) >= 2 && semanticSnapshots.byDir[dir] == nil {
			clear(semanticSnapshots.byDir)
		}
		semanticSnapshots.byDir[dir] = loaded
		semanticSnapshots.Unlock()
		return loaded, nil
	}
	return nil, fmt.Errorf("semantic index changed while loading — retry query")
}

func statIndex(dir string) (indexStamp, error) {
	metaInfo, err := os.Stat(metaPath(dir))
	if err != nil {
		return indexStamp{}, err
	}
	vecInfo, err := os.Stat(vecPath(dir))
	if err != nil {
		return indexStamp{}, err
	}
	return indexStamp{
		metaSize: metaInfo.Size(), metaTime: metaInfo.ModTime().UnixNano(),
		vecSize: vecInfo.Size(), vecTime: vecInfo.ModTime().UnixNano(),
	}, nil
}

func invalidateIndexSnapshot(dir string) {
	dir = filepath.Clean(dir)
	semanticSnapshots.Lock()
	delete(semanticSnapshots.byDir, dir)
	semanticSnapshots.Unlock()
}

func vectorNorm(vector []float32) float64 {
	var squared float64
	for _, value := range vector {
		squared += float64(value) * float64(value)
	}
	return math.Sqrt(squared)
}

func cosineWithNorm(a, b []float32, normA, normB float64) float64 {
	if len(a) != len(b) || normA == 0 || normB == 0 {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot / (normA * normB)
}
