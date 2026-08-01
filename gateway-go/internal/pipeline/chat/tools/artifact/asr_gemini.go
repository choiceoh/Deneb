package artifact

// Frontier-cloud transcription provider (Gemini) — the operator's 2026-08-01
// direction: "ears" benefit from frontier multimodal models more than open
// 0.9B ASR (measured: Gemini nailed 김민준/탑솔라 with NO hotword injection
// where bare MOSS needed hotwords, and fluently transcribed a real meeting
// slice at ~RTF 0.14 including network). Local remains a *preference*, not a
// principle (docs/agent-rules/sidecar-models.md) — this keeps Deneb as the
// brain and rents only the ears.
//
// Provider selection is opt-in via DENEB_ASR_PROVIDER=gemini (unset/"moss" =
// existing sidecar path, byte-for-byte unchanged). When Gemini is primary the
// MOSS sidecar demotes to a fallback: srv1 has had no memory headroom for it
// since qwen36-fast (2026-07-20), so the fallback only fires when the sidecar
// is actually reachable.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const (
	// asrGeminiDefaultModel tracks the vision role's family; override with
	// DENEB_ASR_GEMINI_MODEL when Google ships a better audio tier.
	asrGeminiDefaultModel = "gemini-3.5-flash"
	// asrGeminiInlineMax is the inline_data budget. The API caps requests around
	// 20MB; leave margin for the JSON envelope + base64 growth is accounted for
	// by comparing the RAW audio size (base64 expands 4/3, so 14MB raw ≈ 19MB
	// encoded).
	asrGeminiInlineMax = 14 << 20
)

// asrProvider returns the configured transcription provider: "gemini" or
// "moss" (default). Unknown values fall back to moss so a typo cannot break
// transcription entirely.
func asrProvider() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_ASR_PROVIDER")))
	if v == "gemini" {
		return "gemini"
	}
	return "moss"
}

// asrGeminiKey returns the Google API key (same env the vision provider
// uses). Empty = gemini path unavailable.
func asrGeminiKey() string {
	return strings.TrimSpace(os.Getenv("GOOGLE_API_KEY"))
}

// asrGeminiModel returns the audio transcription model id.
func asrGeminiModel() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_ASR_GEMINI_MODEL")); v != "" {
		return v
	}
	return asrGeminiDefaultModel
}

// asrGeminiBaseURL is overridable for tests (httptest server).
func asrGeminiBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_ASR_GEMINI_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://generativelanguage.googleapis.com"
}

// asrGeminiPrompt asks for the exact rendering formatTranscript produces from
// the sidecar (timestamped, speaker-attributed lines), so both providers hand
// callers the same shape. flat drops the markup entirely — the glasses HUD
// caption path (TranscribeAudioPlain) sends 3-second windows where timestamps
// are noise. Hotwords become context instead of a decoder bias.
func asrGeminiPrompt(hotwords string, flat bool) string {
	var b strings.Builder
	b.WriteString("이 오디오를 한국어로 정확히 전사해줘.\n")
	if flat {
		b.WriteString("타임스탬프·화자 표기 없이 발화 텍스트만 이어서 출력해.\n")
	} else {
		b.WriteString("형식: 각 발화를 `[mm:ss 화자N] 내용` 한 줄로 (화자 구분이 불가능하면 타임스탬프만 `[mm:ss] 내용`).\n")
	}
	b.WriteString("전사 텍스트만 출력하고 설명·머리말은 붙이지 마.\n")
	if h := strings.TrimSpace(hotwords); h != "" {
		fmt.Fprintf(&b, "등장할 수 있는 고유명사(정확히 이 표기로): %s\n", h)
	}
	return b.String()
}

// geminiGenerateBody is the minimal generateContent request shape we emit.
type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inline_data,omitempty"`
	FileData   *geminiFileData   `json:"file_data,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type geminiFileData struct {
	MimeType string `json:"mime_type"`
	FileURI  string `json:"file_uri"`
}

// transcribeAudioGemini sends audio to Gemini generateContent and returns the
// transcript text (diarized format, or flat when flat=true). Small payloads
// ride inline; larger ones go through the Files API first (meeting recordings
// easily exceed the inline budget).
func transcribeAudioGemini(ctx context.Context, audio []byte, mimeType, hotwords string, flat bool) (string, error) {
	if len(audio) == 0 {
		return "", fmt.Errorf("asr: empty audio")
	}
	key := asrGeminiKey()
	if key == "" {
		return "", fmt.Errorf("asr gemini: GOOGLE_API_KEY 미설정")
	}
	mt := normalizeAudioMime(mimeType)

	audioPart := geminiPart{}
	if len(audio) <= asrGeminiInlineMax {
		audioPart.InlineData = &geminiInlineData{
			MimeType: mt,
			Data:     base64.StdEncoding.EncodeToString(audio),
		}
	} else {
		uri, err := geminiUploadFile(ctx, key, audio, mt)
		if err != nil {
			return "", fmt.Errorf("asr gemini 파일 업로드 실패: %w", err)
		}
		audioPart.FileData = &geminiFileData{MimeType: mt, FileURI: uri}
	}

	body, err := json.Marshal(map[string]any{
		"contents": []map[string]any{{
			"parts": []geminiPart{{Text: asrGeminiPrompt(hotwords, flat)}, audioPart},
		}},
	})
	if err != nil {
		return "", err
	}

	runCtx, cancel := context.WithTimeout(ctx, asrTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		asrGeminiBaseURL(), asrGeminiModel(), key)
	req, err := http.NewRequestWithContext(runCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httputil.NewClient(asrTimeout).Do(req)
	if err != nil {
		return "", fmt.Errorf("asr gemini 연결 실패: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
		return "", fmt.Errorf("asr gemini HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("asr gemini 응답 파싱 실패: %w", err)
	}
	var b strings.Builder
	if len(out.Candidates) > 0 { // first candidate only
		for _, p := range out.Candidates[0].Content.Parts {
			b.WriteString(p.Text)
		}
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return "", fmt.Errorf("asr gemini: 빈 전사 응답")
	}
	return text, nil
}

// geminiUploadFile pushes audio through the Files API (raw upload protocol)
// and returns the file URI once it is usable. Audio files activate quickly;
// we poll briefly rather than assume.
func geminiUploadFile(ctx context.Context, key string, audio []byte, mimeType string) (string, error) {
	upCtx, cancel := context.WithTimeout(ctx, asrTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/upload/v1beta/files?key=%s", asrGeminiBaseURL(), key)
	req, err := http.NewRequestWithContext(upCtx, http.MethodPost, url, bytes.NewReader(audio))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Goog-Upload-Protocol", "raw")
	req.Header.Set("Content-Type", mimeType)

	resp, err := httputil.NewClient(asrTimeout).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		File struct {
			Name  string `json:"name"`
			URI   string `json:"uri"`
			State string `json:"state"`
		} `json:"file"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.File.URI == "" {
		return "", fmt.Errorf("업로드 응답에 file.uri 없음")
	}
	// PROCESSING → ACTIVE is usually sub-second for audio; poll a few times so a
	// slow activation doesn't fail the whole transcription.
	state := out.File.State
	for i := 0; i < 10 && state == "PROCESSING"; i++ {
		select {
		case <-upCtx.Done():
			return "", upCtx.Err()
		case <-time.After(2 * time.Second):
		}
		st, err := geminiFileState(upCtx, key, out.File.Name)
		if err != nil {
			break // best-effort: let generateContent surface the real error
		}
		state = st
	}
	if state == "FAILED" {
		return "", fmt.Errorf("파일 처리 실패 (state=FAILED)")
	}
	return out.File.URI, nil
}

// geminiFileState fetches the processing state of an uploaded file.
func geminiFileState(ctx context.Context, key, name string) (string, error) {
	url := fmt.Sprintf("%s/v1beta/%s?key=%s", asrGeminiBaseURL(), name, key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := httputil.NewClient(10 * time.Second).Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.State, nil
}

// normalizeAudioMime maps a caller mime hint onto a type Gemini accepts,
// defaulting to mp3 (the API sniffs content anyway).
func normalizeAudioMime(mimeType string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.Contains(mt, "wav"), strings.Contains(mt, "wave"):
		return "audio/wav"
	case strings.Contains(mt, "ogg"), strings.Contains(mt, "opus"), strings.Contains(mt, "oga"):
		return "audio/ogg"
	case strings.Contains(mt, "m4a"), strings.Contains(mt, "mp4"), strings.Contains(mt, "aac"):
		return "audio/aac"
	case strings.Contains(mt, "flac"):
		return "audio/flac"
	case strings.Contains(mt, "webm"):
		return "audio/webm"
	default:
		return "audio/mp3"
	}
}
