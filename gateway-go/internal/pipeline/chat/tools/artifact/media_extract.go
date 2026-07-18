// media_extract.go — transcribe / ocr: direct agent access to the resident
// GPU sidecars for files ON DISK. The capture RPCs (miniapp.capture.audio/
// image) already transcribe/OCR base64 payloads the app shares, but the agent
// itself had NO path for a file it encounters mid-conversation (a download,
// an exec artifact, something under ~/.deneb/files): the read tool dumps raw
// bytes and the files tool only covers the file store — and audio not at all.
// These two tools close that: point at a path, get text.
package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// transcribeMaxBytes caps the audio payload (60-min meeting recordings in
	// m4a run tens of MB; the ASR server handles up to ~60 min).
	transcribeMaxBytes = 64 << 20
	// ocrMaxBytes caps document/image payloads for the OCR/extract path.
	ocrMaxBytes = 25 << 20
)

// ToolTranscribe returns the transcribe tool: audio file → diarized Korean
// transcript ("[mm:ss 화자N] …") via the resident ASR sidecar (MOSS-Transcribe-Diarize).
// hotwords supplies the wiki+contacts proper-noun bias the capture path
// already uses (people/companies/deals); nil just skips the bias — the
// operator's DENEB_ASR_HOTWORDS env is still merged downstream.
func ToolTranscribe(hotwords func() string) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Path     string `json:"path"`
			Hotwords string `json:"hotwords"`
		}
		if err := jsonutil.UnmarshalInto("transcribe params", input, &p); err != nil {
			return "", err
		}
		data, err := readMediaFile(p.Path, transcribeMaxBytes)
		if err != nil {
			return "", err
		}
		extra := strings.TrimSpace(p.Hotwords)
		if hotwords != nil {
			extra = mergeHotwords(extra, hotwords())
		}
		text, err := transcribeAudioText(ctx, data, audioMimeFromExt(p.Path), extra)
		if err != nil {
			// No local fallback exists for ASR (unlike OCR's tesseract) — a
			// clear failure beats a silent wrong answer.
			return "", fmt.Errorf("transcribe %q: %w", p.Path, err)
		}
		if strings.TrimSpace(text) == "" {
			return fmt.Sprintf("%s: 전사 결과가 비어 있습니다 (무음이거나 지원하지 않는 코덱).", p.Path), nil
		}
		return fmt.Sprintf("%s 전사 결과:\n\n%s", filepath.Base(p.Path), text), nil
	}
}

// ToolOCR returns the ocr tool: image / scanned-PDF / office document on disk
// → extracted text. Routes through document.ExtractText, the same dispatcher the
// mail-attachment and notebook paths use, so images go to PaddleOCR-VL (with
// tesseract fallback), born-digital PDFs to pdftotext with OCR fallback, and
// office formats to their parsers.
func ToolOCR() toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Path string `json:"path"`
		}
		if err := jsonutil.UnmarshalInto("ocr params", input, &p); err != nil {
			return "", err
		}
		data, err := readMediaFile(p.Path, ocrMaxBytes)
		if err != nil {
			return "", err
		}
		text, usedOCR, err := document.ExtractText(ctx, data, filepath.Base(p.Path), "")
		if err != nil {
			return "", fmt.Errorf("ocr %q: %w", p.Path, err)
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return fmt.Sprintf("%s: 추출된 텍스트가 없습니다 (빈 문서이거나 지원하지 않는 형식).", p.Path), nil
		}
		label := "텍스트 추출"
		if usedOCR {
			label = "OCR"
		}
		return fmt.Sprintf("%s %s 결과:\n\n%s", filepath.Base(p.Path), label, text), nil
	}
}

// readMediaFile loads a media file with a size cap checked BEFORE reading, so
// a stray multi-GB path cannot balloon memory. Absolute paths expected (the
// agent gets them from exec/files/captures); relative ones resolve against
// the gateway's working directory as-is.
func readMediaFile(path string, maxBytes int64) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path가 필요합니다 (디스크의 파일 절대 경로)")
	}
	if err := CheckProtectedPath(path, "read"); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("파일을 열 수 없습니다: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s: 디렉토리입니다 — 파일 경로를 지정하세요", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("%s: 파일이 너무 큽니다 (%d bytes > %d 제한)", path, info.Size(), maxBytes)
	}
	return os.ReadFile(path)
}

// audioMimeFromExt maps a filename extension to the mime hint the ASR client
// uses to pick a multipart filename (the server sniffs the real codec from
// bytes, so an unknown extension is fine — empty hint).
func audioMimeFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".m4a", ".mp4", ".aac":
		return "audio/mp4"
	case ".mp3":
		return "audio/mpeg"
	case ".oga", ".ogg", ".opus":
		return "audio/ogg"
	case ".wav":
		return "audio/wav"
	case ".webm":
		return "audio/webm"
	case ".flac":
		return "audio/flac"
	default:
		return ""
	}
}
