package artifact

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeGemini serves generateContent (and optionally the Files API) and records
// what it saw.
type fakeGemini struct {
	t            *testing.T
	text         string
	status       int
	sawGenerate  atomic.Int32
	sawUpload    atomic.Int32
	lastPrompt   atomic.Value // string
	lastMimeHint atomic.Value // string
}

func (f *fakeGemini) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/upload/v1beta/files"):
			f.sawUpload.Add(1)
			f.lastMimeHint.Store(r.Header.Get("Content-Type"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"file": map[string]any{"name": "files/abc", "uri": "https://files/abc", "state": "ACTIVE"},
			})
		case strings.Contains(r.URL.Path, ":generateContent"):
			f.sawGenerate.Add(1)
			var body struct {
				Contents []struct {
					Parts []struct {
						Text       string `json:"text"`
						InlineData *struct {
							MimeType string `json:"mime_type"`
						} `json:"inline_data"`
						FileData *struct {
							FileURI string `json:"file_uri"`
						} `json:"file_data"`
					} `json:"parts"`
				} `json:"contents"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Contents) > 0 && len(body.Contents[0].Parts) > 0 {
				f.lastPrompt.Store(body.Contents[0].Parts[0].Text)
			}
			if f.status != 0 && f.status != http.StatusOK {
				w.WriteHeader(f.status)
				_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{{
					"content": map[string]any{"parts": []map[string]any{{"text": f.text}}},
				}},
			})
		default:
			f.t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// fakeMoss serves /health + /v1/transcribe.
type fakeMoss struct {
	text  string
	calls atomic.Int32
}

func (f *fakeMoss) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/transcribe":
			f.calls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"transcription": f.text})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func setASREnv(t *testing.T, provider, geminiURL, mossURL string) {
	t.Helper()
	t.Setenv("DENEB_ASR_PROVIDER", provider)
	t.Setenv("DENEB_ASR_GEMINI_URL", geminiURL)
	t.Setenv("DENEB_ASR_URL", mossURL)
	t.Setenv("GOOGLE_API_KEY", "test-key")
	t.Setenv("DENEB_ASR_HOTWORDS", "")
}

func TestTranscribeGeminiPrimarySucceeds(t *testing.T) {
	g := &fakeGemini{t: t, text: "[00:01 화자1] 탑솔라 견적 보고"}
	gs := g.server()
	defer gs.Close()
	m := &fakeMoss{text: "moss-should-not-run"}
	ms := m.server()
	defer ms.Close()
	setASREnv(t, "gemini", gs.URL, ms.URL)

	out, err := transcribeAudioText(context.Background(), []byte("audio-bytes"), "audio/mp3", "탑솔라")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "[00:01 화자1] 탑솔라 견적 보고" {
		t.Fatalf("unexpected transcript: %q", out)
	}
	if m.calls.Load() != 0 {
		t.Fatalf("moss must not be called when gemini succeeds")
	}
	prompt, _ := g.lastPrompt.Load().(string)
	if !strings.Contains(prompt, "탑솔라") {
		t.Fatalf("hotwords must reach the gemini prompt, got %q", prompt)
	}
	if strings.Contains(prompt, "화자 표기 없이") {
		t.Fatalf("diarized path must not request flat output")
	}
}

func TestTranscribeGeminiFailureFallsBackToReachableMoss(t *testing.T) {
	g := &fakeGemini{t: t, status: http.StatusInternalServerError}
	gs := g.server()
	defer gs.Close()
	m := &fakeMoss{text: "모스 폴백 전사"}
	ms := m.server()
	defer ms.Close()
	setASREnv(t, "gemini", gs.URL, ms.URL)

	out, err := transcribeAudioText(context.Background(), []byte("audio-bytes"), "audio/mp3", "")
	if err != nil {
		t.Fatalf("fallback should succeed, got %v", err)
	}
	if out != "모스 폴백 전사" {
		t.Fatalf("unexpected transcript: %q", out)
	}
	if m.calls.Load() != 1 {
		t.Fatalf("moss fallback expected exactly once, got %d", m.calls.Load())
	}
}

func TestTranscribeGeminiFailureWithDeadMossSurfacesGeminiError(t *testing.T) {
	g := &fakeGemini{t: t, status: http.StatusForbidden}
	gs := g.server()
	defer gs.Close()
	setASREnv(t, "gemini", gs.URL, "http://127.0.0.1:1") // dead sidecar

	_, err := transcribeAudioText(context.Background(), []byte("audio-bytes"), "audio/mp3", "")
	if err == nil || !strings.Contains(err.Error(), "gemini") {
		t.Fatalf("expected the gemini error to surface, got %v", err)
	}
}

func TestTranscribeDefaultProviderNeverTouchesGemini(t *testing.T) {
	g := &fakeGemini{t: t, text: "should-not-run"}
	gs := g.server()
	defer gs.Close()
	m := &fakeMoss{text: "사이드카 전사"}
	ms := m.server()
	defer ms.Close()
	setASREnv(t, "", gs.URL, ms.URL) // provider unset → moss

	out, err := transcribeAudioText(context.Background(), []byte("audio-bytes"), "audio/mp3", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "사이드카 전사" {
		t.Fatalf("unexpected transcript: %q", out)
	}
	if g.sawGenerate.Load() != 0 {
		t.Fatalf("gemini must not be called with the default provider")
	}
}

func TestTranscribeGeminiLargeAudioUsesFilesAPI(t *testing.T) {
	g := &fakeGemini{t: t, text: "긴 회의 전사"}
	gs := g.server()
	defer gs.Close()
	setASREnv(t, "gemini", gs.URL, "http://127.0.0.1:1")

	big := make([]byte, asrGeminiInlineMax+1)
	out, err := transcribeAudioGemini(context.Background(), big, "audio/m4a", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "긴 회의 전사" {
		t.Fatalf("unexpected transcript: %q", out)
	}
	if g.sawUpload.Load() != 1 {
		t.Fatalf("large audio must go through the Files API")
	}
	if mt, _ := g.lastMimeHint.Load().(string); mt != "audio/aac" {
		t.Fatalf("m4a should normalize to audio/aac, got %q", mt)
	}
}

func TestTranscribePlainRequestsFlatOutput(t *testing.T) {
	g := &fakeGemini{t: t, text: "자막 텍스트"}
	gs := g.server()
	defer gs.Close()
	setASREnv(t, "gemini", gs.URL, "http://127.0.0.1:1")

	out, err := TranscribeAudioPlain(context.Background(), []byte("audio"), "audio/wav", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "자막 텍스트" {
		t.Fatalf("unexpected transcript: %q", out)
	}
	prompt, _ := g.lastPrompt.Load().(string)
	if !strings.Contains(prompt, "화자 표기 없이") {
		t.Fatalf("plain path must request flat output, got %q", prompt)
	}
}

// TestTranscribeGeminiLive is an opt-in end-to-end probe against the real API:
// DENEB_ASR_GEMINI_LIVE=1 DENEB_ASR_GEMINI_AUDIO=/path.mp3 GOOGLE_API_KEY=… go test -run GeminiLive ./...
func TestTranscribeGeminiLive(t *testing.T) {
	if os.Getenv("DENEB_ASR_GEMINI_LIVE") != "1" {
		t.Skip("live test: set DENEB_ASR_GEMINI_LIVE=1")
	}
	path := os.Getenv("DENEB_ASR_GEMINI_AUDIO")
	audio, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out, err := transcribeAudioGemini(context.Background(), audio, "audio/mp3", "탑솔라, 데네브", false)
	if err != nil {
		t.Fatalf("live transcription failed: %v", err)
	}
	t.Logf("live transcript:\n%s", out)
	if strings.TrimSpace(out) == "" {
		t.Fatal("empty live transcript")
	}
}

// The sidecar liveness watch keys off primary-ness: gemini primary means the
// sidecar is a deliberate fallback whose death must not page the operator.
func TestASRSidecarIsPrimary(t *testing.T) {
	t.Setenv("DENEB_ASR_PROVIDER", "gemini")
	if ASRSidecarIsPrimary() {
		t.Fatal("gemini primary: sidecar must not be watched as primary")
	}
	t.Setenv("DENEB_ASR_PROVIDER", "")
	if !ASRSidecarIsPrimary() {
		t.Fatal("no provider override: sidecar is the primary path")
	}
}
