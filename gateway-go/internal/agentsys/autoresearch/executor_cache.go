package autoresearch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// --- Metric caching ---

// contentHash computes a deterministic SHA-256 hash of all target files.
// Identical file contents produce the same hash regardless of iteration.
func contentHash(workdir string, targetFiles []string) (string, error) {
	sorted := make([]string, len(targetFiles))
	copy(sorted, targetFiles)
	sort.Strings(sorted)

	h := sha256.New()
	for _, f := range sorted {
		data, err := os.ReadFile(filepath.Join(workdir, f))
		if err != nil {
			return "", fmt.Errorf("read %s for hash: %w", f, err)
		}
		h.Write([]byte(f))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// metricCacheEntry is the on-disk format for cached metric results.
type metricCacheEntry struct {
	Metric    float64 `json:"metric"`
	MetricCmd string  `json:"metric_cmd"`
	Timestamp string  `json:"timestamp"`
}

// loadCachedMetric checks if a metric result is cached for the given content hash.
// Returns the cached metric and true if found and the metric_cmd matches.
func loadCachedMetric(cacheDir, hash, metricCmd string) (float64, bool) {
	path := filepath.Join(cacheDir, "results", hash+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var entry metricCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return 0, false
	}
	// Invalidate if the metric command changed.
	if entry.MetricCmd != metricCmd {
		return 0, false
	}
	return entry.Metric, true
}

// saveCachedMetric persists a metric result for the given content hash.
func saveCachedMetric(cacheDir, hash, metricCmd string, metric float64) error {
	dir := filepath.Join(cacheDir, "results")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entry := metricCacheEntry{
		Metric:    metric,
		MetricCmd: metricCmd,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, hash+".json"), data, 0o644)
}

// overrideHash computes a deterministic hash for constants-mode overrides.
func overrideHash(base string, overrides map[string]string) string {
	h := sha256.New()
	h.Write([]byte(base))
	h.Write([]byte{0})

	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte("="))
		h.Write([]byte(overrides[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
