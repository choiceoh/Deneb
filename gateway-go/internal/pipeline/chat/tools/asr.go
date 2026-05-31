package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// VibeVoice-ASR is Deneb's speech-to-text sidecar: microsoft/VibeVoice-ASR-HF
// (transformers, served on the local GPU by start-vibevoice-asr.sh, port 18013).
// One model does transcription + speaker diarization + timestamps, and accepts
// `hotwords` (Deneb contacts / deals / company names) that reliably fix the
// Korean proper nouns bare ASR mis-hears (탑솔라, 데네브). It decodes any
// container (Telegram .oga/opus, m4a, mp3, wav) to 24 kHz mono internally.
// Unlike the OCR path there is no local fallback — when the server is down,
// transcription returns a clear error and the caller degrades by surfacing it.

const (
	// asrDefaultURL is the local VibeVoice-ASR FastAPI base.
	asrDefaultURL = "http://127.0.0.1:18013"
	// asrTimeout bounds a single transcription. The model is serialized
	// (single-user) and a long recording takes a while; the generous ceiling
	// stays under the 5-minute agent turn deadline.
	asrTimeout = 4 * time.Minute
)

// asrBaseURL returns the ASR server base URL, overridable via DENEB_ASR_URL for
// tests or a non-default deployment.
func asrBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_ASR_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return asrDefaultURL
}

// asrHotwords returns the optional proper-noun bias list. The operator can set
// DENEB_ASR_HOTWORDS to a comma/space list of names (탑솔라, 데네브, contacts)
// to correct Korean proper nouns; empty is fine.
func asrHotwords() string {
	return strings.TrimSpace(os.Getenv("DENEB_ASR_HOTWORDS"))
}

// asrSegment is one diarized span: who said what, when.
type asrSegment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker flexStr `json:"speaker"`
	Content string  `json:"content"`
}

// flexStr decodes a JSON string OR number into a string. The ASR model emits a
// segment's speaker as either a label ("SPEAKER_00") or a bare diarization
// index (0, 1, ...), so a plain `string` field would fail to unmarshal the
// numeric case. null decodes to "".
type flexStr string

func (f *flexStr) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexStr(s)
		return nil
	}
	// A bare number (or anything else) is used verbatim; trim a trailing ".0"
	// so a float index like 1.0 reads as "1".
	*f = flexStr(strings.TrimSuffix(string(b), ".0"))
	return nil
}

// asrResponse mirrors vibevoice_server.py's /v1/transcribe JSON.
type asrResponse struct {
	Segments      []asrSegment `json:"segments"`
	Transcription string       `json:"transcription"`
	Raw           string       `json:"raw"`
	DurationSec   float64      `json:"duration_sec"`
	InferSec      float64      `json:"infer_sec"`
	RTF           float64      `json:"rtf"`
	Hotwords      string       `json:"hotwords"`
}

// transcribeAudio posts raw audio bytes to VibeVoice-ASR and returns the parsed
// structured response (segments + flat transcription). filename is cosmetic —
// the server sniffs the codec from the bytes — but a plausible extension keeps
// the multipart part tidy.
func transcribeAudio(ctx context.Context, audio []byte, filename, hotwords string) (*asrResponse, error) {
	if len(audio) == 0 {
		return nil, fmt.Errorf("vibevoice-asr: empty audio")
	}
	if strings.TrimSpace(filename) == "" {
		filename = "audio"
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, err
	}
	if h := strings.TrimSpace(hotwords); h != "" {
		if err := w.WriteField("hotwords", h); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, asrTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(runCtx, http.MethodPost,
		asrBaseURL()+"/v1/transcribe", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := httputil.NewClient(asrTimeout).Do(req)
	if err != nil {
		return nil, fmt.Errorf("vibevoice-asr 연결 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
		return nil, fmt.Errorf("vibevoice-asr HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var out asrResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("vibevoice-asr 응답 파싱 실패: %w", err)
	}
	return &out, nil
}

// formatTranscript renders a diarized, timestamped transcript when the model
// returned segments (the value over a flat string — a meeting becomes "who said
// what, when"); otherwise it falls back to the flat transcription.
func formatTranscript(r *asrResponse) string {
	if r == nil {
		return ""
	}
	segs := append([]asrSegment(nil), r.Segments...)
	// Keep chronological order even if the model emits spans out of order.
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].Start < segs[j].Start })
	var b strings.Builder
	for _, s := range segs {
		content := strings.TrimSpace(s.Content)
		if content == "" {
			continue
		}
		fmt.Fprintf(&b, "[%s %s] %s\n", mmss(s.Start), displaySpeaker(string(s.Speaker)), content)
	}
	if out := strings.TrimSpace(b.String()); out != "" {
		return out
	}
	return strings.TrimSpace(r.Transcription)
}

// displaySpeaker renders a segment's speaker for humans: a bare diarization
// index (0, 1, ...) becomes 화자1/화자2 (1-based, friendlier than "0"); a
// string label ("SPEAKER_01", or a name) is kept as-is; empty falls back to 화자.
func displaySpeaker(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "화자"
	}
	if n, err := strconv.Atoi(s); err == nil {
		return fmt.Sprintf("화자%d", n+1)
	}
	return s
}

// mmss formats seconds as mm:ss (or h:mm:ss past an hour).
func mmss(sec float64) string {
	if math.IsNaN(sec) || math.IsInf(sec, 0) || sec < 0 {
		sec = 0
	}
	total := int(sec + 0.5)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// transcribeAudioText is the single transcription entry point: it calls
// VibeVoice-ASR and returns a ready-to-read diarized transcript. mimeType is a
// hint used only to pick the multipart filename extension. There is no local
// fallback — an unreachable server surfaces as an error the caller reports.
func transcribeAudioText(ctx context.Context, audio []byte, mimeType, extraHotwords string) (string, error) {
	r, err := transcribeAudio(ctx, audio, audioFilename(mimeType), mergeHotwords(extraHotwords, asrHotwords()))
	if err != nil {
		return "", err
	}
	return formatTranscript(r), nil
}

// mergeHotwords joins caller-supplied hotwords (e.g. wiki proper nouns) with the
// operator's DENEB_ASR_HOTWORDS, dropping blanks. Caller hints come first.
func mergeHotwords(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, ", ")
}

// audioFilename maps a mime type to a plausible multipart filename. The server
// sniffs the real codec, so this only needs to look reasonable.
func audioFilename(mimeType string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.Contains(mt, "mp4"), strings.Contains(mt, "m4a"), strings.Contains(mt, "aac"):
		return "audio.m4a"
	case strings.Contains(mt, "mpeg"), strings.Contains(mt, "mp3"):
		return "audio.mp3"
	case strings.Contains(mt, "ogg"), strings.Contains(mt, "opus"), strings.Contains(mt, "oga"):
		return "audio.oga"
	case strings.Contains(mt, "wav"), strings.Contains(mt, "wave"):
		return "audio.wav"
	case strings.Contains(mt, "webm"):
		return "audio.webm"
	case strings.Contains(mt, "flac"):
		return "audio.flac"
	default:
		return "audio"
	}
}
