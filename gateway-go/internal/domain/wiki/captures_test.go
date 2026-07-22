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

// TestSaveCaptureAtUniquePathsPerBatch locks the multi-file batch contract: two
// same-kind captures saved back-to-back (same second) must land in distinct files,
// so the batch turn's per-file pointers each open their own file instead of the
// second silently overwriting the first.
func TestSaveCaptureAtUniquePathsPerBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "memory", "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	relA, absA, _, err := store.SaveCaptureAt("document", "계약서 A", "양수도 대금 10억원")
	if err != nil {
		t.Fatalf("SaveCaptureAt A: %v", err)
	}
	relB, absB, _, err := store.SaveCaptureAt("document", "계약서 B", "양수도 대금 20억원")
	if err != nil {
		t.Fatalf("SaveCaptureAt B: %v", err)
	}
	if relA == relB || absA == absB {
		t.Fatalf("same-kind batch captures collided on one path: %q", relA)
	}
	dataA, _ := os.ReadFile(absA)
	dataB, _ := os.ReadFile(absB)
	if !strings.Contains(string(dataA), "10억원") {
		t.Errorf("file A was overwritten — missing its own content: %q", string(dataA))
	}
	if !strings.Contains(string(dataB), "20억원") {
		t.Errorf("file B missing its own content: %q", string(dataB))
	}
}

// TestSaveCaptureAtBodyLineAlignsWithFile locks the digest-map contract: the
// returned bodyStartLine must point at the exact file line where the
// normalized body begins, with and without the optional context header line.
func TestSaveCaptureAtBodyLineAlignsWithFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "memory", "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	body := "1번째 줄\n2번째 줄\n3번째 줄"
	for _, tc := range []struct {
		name, context string
	}{
		{"with context", "IM 문서"},
		{"without context", ""},
	} {
		_, abs, bodyLine, err := store.SaveCaptureAt("document", tc.context, "  \n"+body+"\n\n")
		if err != nil {
			t.Fatalf("%s: SaveCaptureAt: %v", tc.name, err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			t.Fatalf("%s: read capture: %v", tc.name, err)
		}
		fileLines := strings.Split(string(data), "\n")
		bodyLines := strings.Split(NormalizeCaptureText(body), "\n")
		for i, want := range bodyLines {
			idx := bodyLine - 1 + i
			if idx >= len(fileLines) || fileLines[idx] != want {
				t.Errorf("%s: file line %d = %q, want %q", tc.name, bodyLine+i, fileLines[idx], want)
			}
		}
	}
}
