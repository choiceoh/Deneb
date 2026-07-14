package artifact

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadMediaFile_Guards(t *testing.T) {
	if _, err := readMediaFile("", 100); err == nil {
		t.Error("empty path must error")
	}
	if _, err := readMediaFile(filepath.Join(t.TempDir(), "nope.m4a"), 100); err == nil {
		t.Error("missing file must error")
	}
	if _, err := readMediaFile(t.TempDir(), 100); err == nil {
		t.Error("directory must error")
	}
	big := filepath.Join(t.TempDir(), "big.wav")
	if err := os.WriteFile(big, make([]byte, 200), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readMediaFile(big, 100); err == nil || !strings.Contains(err.Error(), "너무 큽니다") {
		t.Errorf("oversized file must be rejected before reading, got %v", err)
	}
}

func TestAudioMimeFromExtReturnsExpectedMime(t *testing.T) {
	cases := map[string]string{
		"a.m4a": "audio/mp4", "b.MP3": "audio/mpeg", "c.oga": "audio/ogg",
		"d.wav": "audio/wav", "e.flac": "audio/flac", "f.unknown": "",
	}
	for path, want := range cases {
		if got := audioMimeFromExt(path); got != want {
			t.Errorf("%s → %q, want %q", path, got, want)
		}
	}
}

// A plain-text file routes through document.ExtractText's text branch — the OCR
// tool's plumbing is testable without the GPU sidecars.
func TestToolOCRReadsTextFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "메모.txt")
	if err := os.WriteFile(path, []byte("납품 기한은 7월 11일"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := ToolOCR()(context.Background(), json.RawMessage(`{"path":"`+path+`"}`))
	if err != nil {
		t.Fatalf("ocr txt: %v", err)
	}
	if !strings.Contains(out, "납품 기한은 7월 11일") {
		t.Errorf("text not extracted: %q", out)
	}
}

func TestToolTranscribe_MissingFile(t *testing.T) {
	_, err := ToolTranscribe(nil)(context.Background(),
		json.RawMessage(`{"path":"`+filepath.Join(t.TempDir(), "gone.m4a")+`"}`))
	if err == nil {
		t.Error("missing audio file must error")
	}
}
