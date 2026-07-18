package document

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// OCR results are content-addressed and deterministic (temperature 0 against
// a fixed engine), so a small disk cache turns repeat OCR of the same bytes —
// mail-poll analysis, the user opening the same attachment in chat, document
// re-asks — into a free read instead of seconds of GPU decode. Only healthy
// PaddleOCR-VL results are cached: tesseract fallbacks and still-looped
// last-resort outputs stay uncached so a recovered server gets to redo them.

const (
	// ocrCacheVersion prefixes every entry; bump when the post-processing
	// (table conversion, markdown rendering) changes so stale renderings die.
	ocrCacheVersion = "v1"
	// ocrCacheMaxEntries bounds the directory; the oldest overflow is pruned
	// on write. Entries are small text files (a few KB each).
	ocrCacheMaxEntries = 4096
	ocrCachePruneBatch = 256
)

// ocrCacheDir resolves the cache directory, creating it on first use.
// DENEB_OCR_CACHE_DIR overrides (tests, non-default deployments); an empty
// resolution disables caching entirely (fail-open — OCR still works).
func ocrCacheDir() string {
	dir := strings.TrimSpace(os.Getenv("DENEB_OCR_CACHE_DIR"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".deneb", "cache", "ocr")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}

func ocrCachePath(dir string, img []byte) string {
	sum := sha256.Sum256(img)
	return filepath.Join(dir, ocrCacheVersion+"_"+hex.EncodeToString(sum[:])+".txt")
}

func ocrCacheGet(img []byte) (string, bool) {
	dir := ocrCacheDir()
	if dir == "" {
		return "", false
	}
	data, err := os.ReadFile(ocrCachePath(dir, img))
	if err != nil || len(data) == 0 {
		return "", false
	}
	return string(data), true
}

func ocrCachePut(img []byte, text string) {
	dir := ocrCacheDir()
	if dir == "" || strings.TrimSpace(text) == "" {
		return
	}
	path := ocrCachePath(dir, img)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(text), 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return
	}
	pruneOCRCache(dir)
}

// pruneOCRCache drops the oldest entries once the directory overflows.
// Attachment OCR is low-rate, so the per-put ReadDir is negligible.
func pruneOCRCache(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= ocrCacheMaxEntries {
		return
	}
	type aged struct {
		name string
		mod  int64
	}
	files := make([]aged, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		files = append(files, aged{e.Name(), info.ModTime().UnixNano()})
	}
	if len(files) <= ocrCacheMaxEntries {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod < files[j].mod })
	drop := len(files) - ocrCacheMaxEntries + ocrCachePruneBatch
	if drop > len(files) {
		drop = len(files)
	}
	for _, f := range files[:drop] {
		_ = os.Remove(filepath.Join(dir, f.name))
	}
}
