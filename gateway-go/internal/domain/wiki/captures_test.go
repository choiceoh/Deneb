package wiki

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveCapture_FileAndBreadcrumb(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "memory", "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	transcript := "[00:01 화자1] 견적은 6월 20일까지 보내기로 합니다.\n[00:05 화자2] 단가는 kW당 32만원으로 확정."
	rel, err := store.SaveCapture("audio", "현대차 미팅 녹음", transcript)
	if err != nil {
		t.Fatalf("SaveCapture: %v", err)
	}
	if !strings.HasPrefix(rel, "captures/") || !strings.HasSuffix(rel, "-audio.md") {
		t.Errorf("unexpected rel path %q", rel)
	}

	// Full original on disk, under the memory root (so the backup ships it).
	abs := filepath.Join(dir, "memory", rel)
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("capture file missing: %v", err)
	}
	for _, want := range []string{"32만원", "현대차 미팅 녹음", "캡처 원문"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("capture file missing %q", want)
		}
	}

	// Diary breadcrumb is immediately searchable and points at the file.
	hits, err := store.SearchDiary(context.Background(), "견적", 4)
	if err != nil || len(hits) == 0 {
		t.Fatalf("breadcrumb not searchable: %v %+v", err, hits)
	}
	if !strings.Contains(hits[0].Content, rel) {
		t.Errorf("breadcrumb missing capture path: %q", hits[0].Content)
	}

	// Empty text is rejected.
	if _, err := store.SaveCapture("image", "", "   "); err == nil {
		t.Error("empty capture must be rejected")
	}
}
