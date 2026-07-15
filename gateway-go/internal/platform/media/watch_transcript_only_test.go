package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWatchLocalFile_TranscriptOnlyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("not-a-real-video"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := WatchVideo(context.Background(), path, WatchOptions{TranscriptOnly: true})
	if err == nil {
		t.Fatal("expected error for transcript-only local file")
	}
	if !strings.Contains(err.Error(), "transcript-only") {
		t.Errorf("error = %v, want transcript-only mention", err)
	}
}
