package translateops

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps the durable translation cache out of the real state dir: these
// tests write translations, and a shared file would both pollute the operator's
// cache and leak results between tests.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "translateops-cache")
	if err != nil {
		panic(err)
	}
	translateDisk = &translateDiskCache{pathOverride: filepath.Join(dir, "translate-cache.json")}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// resetTranslateTextCache starts both layers over. The durable layer has to go
// too — and its file with it: a test that only emptied the hot set would still
// be answered from disk, which is the whole point of the disk being there.
func resetTranslateTextCache() {
	translateTextCache.Clear()
	path := translateDisk.path()
	_ = os.Remove(path)
	translateDisk = &translateDiskCache{pathOverride: path}
}
