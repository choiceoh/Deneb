package document

import (
	"path/filepath"
	"testing"
)

// The OCR cache used to be built from $HOME, so a dev gateway (real $HOME,
// DENEB_STATE_DIR=/tmp/…) wrote — and overflow-pruned — the production cache.
func TestOCRCacheDirFollowsTheStateDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DENEB_OCR_CACHE_DIR", "")

	stateDir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", stateDir)
	if got, want := ocrCacheDir(), filepath.Join(stateDir, "cache", "ocr"); got != want {
		t.Fatalf("dev OCR cache = %q, want %q", got, want)
	}

	// Production pins DENEB_STATE_DIR=$HOME/.deneb, and unset defaults there.
	t.Setenv("DENEB_STATE_DIR", "")
	if got, want := ocrCacheDir(), filepath.Join(home, ".deneb", "cache", "ocr"); got != want {
		t.Fatalf("production OCR cache = %q, want %q", got, want)
	}
}
