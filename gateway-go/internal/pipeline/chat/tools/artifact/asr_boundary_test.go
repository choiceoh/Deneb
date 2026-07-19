package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type capturedASRRequest struct {
	method   string
	path     string
	filename string
	audio    string
	hotwords string
}

func captureASRRequest(r *http.Request) (capturedASRRequest, error) {
	var got capturedASRRequest
	got.method = r.Method
	got.path = r.URL.Path
	reader, err := r.MultipartReader()
	if err != nil {
		return got, err
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return got, err
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return got, err
		}
		switch part.FormName() {
		case "file":
			got.filename = part.FileName()
			got.audio = string(data)
		case "hotwords":
			got.hotwords = string(data)
		}
	}
	return got, nil
}

func asrTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("DENEB_ASR_URL", srv.URL+"///")
	return srv
}

func TestFlexStrStrictJSONBoundary(t *testing.T) {
	valid := []struct {
		name string
		json string
		want flexStr
	}{
		{name: "string", json: `"SPEAKER_00"`, want: "SPEAKER_00"},
		{name: "escaped string", json: `"김\n리드"`, want: "김\n리드"},
		{name: "integer", json: `2`, want: "2"},
		{name: "integral float", json: `10.0`, want: "10"},
		{name: "fraction", json: `1.25`, want: "1.25"},
		{name: "negative", json: `-1`, want: "-1"},
		{name: "exponent", json: `1e2`, want: "1e2"},
		{name: "null", json: `null`, want: ""},
	}
	for _, tc := range valid {
		t.Run(tc.name, func(t *testing.T) {
			var got flexStr
			if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
				t.Fatalf("Unmarshal(%s): %v", tc.json, err)
			}
			if got != tc.want {
				t.Fatalf("Unmarshal(%s) = %q, want %q", tc.json, got, tc.want)
			}
		})
	}

	invalid := []string{`true`, `false`, `{}`, `[]`, `{"speaker":1}`, ``, `"unterminated`}
	for _, raw := range invalid {
		t.Run("reject_"+strings.ReplaceAll(raw, "/", "_"), func(t *testing.T) {
			var got flexStr
			if err := json.Unmarshal([]byte(raw), &got); err == nil {
				t.Fatalf("Unmarshal(%q) succeeded with %q; malformed speaker must fail", raw, got)
			}
		})
	}
}

func TestASREnvironmentNormalizesURLAndHotwords(t *testing.T) {
	t.Setenv("DENEB_ASR_URL", "  http://asr.example.test/base///  ")
	if got := asrBaseURL(); got != "http://asr.example.test/base" {
		t.Fatalf("asrBaseURL = %q", got)
	}
	t.Setenv("DENEB_ASR_HOTWORDS", "  데네브, 탑솔라  ")
	if got := asrHotwords(); got != "데네브, 탑솔라" {
		t.Fatalf("asrHotwords = %q", got)
	}

	t.Setenv("DENEB_ASR_URL", "")
	if got := asrBaseURL(); got != asrDefaultURL {
		t.Fatalf("empty override = %q, want %q", got, asrDefaultURL)
	}
}

func TestASRReadyHonorsHealthStatusAndContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   bool
	}{
		{name: "ready", status: http.StatusOK, want: true},
		{name: "starting", status: http.StatusServiceUnavailable, want: false},
		{name: "redirect is not readiness", status: http.StatusTemporaryRedirect, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan *http.Request, 1)
			asrTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				seen <- r.Clone(r.Context())
				w.WriteHeader(tc.status)
			})
			if got := asrReady(context.Background()); got != tc.want {
				t.Fatalf("asrReady = %v, want %v", got, tc.want)
			}
			r := <-seen
			if r.Method != http.MethodGet || r.URL.Path != "/health" {
				t.Fatalf("health request = %s %s", r.Method, r.URL.Path)
			}
		})
	}

	t.Run("already canceled", func(t *testing.T) {
		var requests int
		asrTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			requests++
			w.WriteHeader(http.StatusOK)
		})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if asrReady(ctx) {
			t.Fatal("canceled readiness probe reported ready")
		}
		if requests != 0 {
			t.Fatalf("canceled probe reached server %d times", requests)
		}
	})
}

func TestTranscribeAudioMultipartRoundTrip(t *testing.T) {
	captured := make(chan capturedASRRequest, 1)
	asrTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got, err := captureASRRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		captured <- got
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"transcription":"flat fallback",
			"segments":[
				{"start":4.5,"end":5,"speaker":1,"content":" 둘째 "},
				{"start":0,"end":2,"speaker":"SPEAKER_00","content":"첫째"}
			],
			"duration_sec":5.0,
			"hotwords":"데네브"
		}`)
	})

	resp, err := transcribeAudio(context.Background(), []byte("audio-bytes"), "meeting.oga", "  데네브  ")
	if err != nil {
		t.Fatalf("transcribeAudio: %v", err)
	}
	got := <-captured
	if got.method != http.MethodPost || got.path != "/v1/transcribe" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if got.filename != "meeting.oga" || got.audio != "audio-bytes" || got.hotwords != "데네브" {
		t.Fatalf("multipart mismatch: %+v", got)
	}
	if resp.DurationSec != 5 || len(resp.Segments) != 2 || resp.Segments[0].Speaker != "1" {
		t.Fatalf("response mismatch: %+v", resp)
	}
	if text := formatTranscript(resp); text != "[00:00 SPEAKER_00] 첫째\n[00:05 화자2] 둘째" {
		t.Fatalf("formatted transcript = %q", text)
	}
	// Formatting sorts a copy; callers retain the server's original order.
	if resp.Segments[0].Start != 4.5 {
		t.Fatalf("formatTranscript mutated response ordering: %+v", resp.Segments)
	}
}

func TestTranscribeAudioUsesDefaultFilenameWithoutHotwordsField(t *testing.T) {
	captured := make(chan capturedASRRequest, 1)
	asrTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got, err := captureASRRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		captured <- got
		_, _ = io.WriteString(w, `{"transcription":"ok"}`)
	})
	if _, err := transcribeAudio(context.Background(), []byte{1, 2, 3}, "  ", " \n "); err != nil {
		t.Fatalf("transcribeAudio: %v", err)
	}
	got := <-captured
	if got.filename != "audio" {
		t.Fatalf("default filename = %q", got.filename)
	}
	if got.hotwords != "" {
		t.Fatalf("blank hotwords should omit field, got %q", got.hotwords)
	}
}

func TestTranscribeAudioRejectsEmptyBeforeNetwork(t *testing.T) {
	var requests int
	asrTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	})
	if _, err := transcribeAudio(context.Background(), nil, "x.wav", ""); err == nil || !strings.Contains(err.Error(), "empty audio") {
		t.Fatalf("empty audio err = %v", err)
	}
	if requests != 0 {
		t.Fatalf("empty audio unexpectedly made %d request(s)", requests)
	}
}

func TestTranscribeAudioExternalFailures(t *testing.T) {
	t.Run("bounded status body", func(t *testing.T) {
		asrTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, strings.Repeat("x", 10_000))
		})
		_, err := transcribeAudio(context.Background(), []byte("a"), "a.wav", "")
		if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
			t.Fatalf("status err = %v", err)
		}
		if len(err.Error()) > 2_200 {
			t.Fatalf("status body was not bounded: %d chars", len(err.Error()))
		}
	})

	t.Run("malformed response", func(t *testing.T) {
		asrTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"segments":[`)
		})
		_, err := transcribeAudio(context.Background(), []byte("a"), "a.wav", "")
		if err == nil || !strings.Contains(err.Error(), "응답 파싱 실패") {
			t.Fatalf("parse err = %v", err)
		}
	})

	t.Run("invalid speaker shape rejects whole response", func(t *testing.T) {
		asrTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"segments":[{"speaker":{"id":1},"content":"bad"}]}`)
		})
		_, err := transcribeAudio(context.Background(), []byte("a"), "a.wav", "")
		if err == nil || !strings.Contains(err.Error(), "speaker must be") {
			t.Fatalf("speaker shape err = %v", err)
		}
	})
}

func TestTranscribeAudioCancellationPropagatesInFlight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	requestCanceled := make(chan struct{})
	defer close(release)
	asrTestServer(t, func(_ http.ResponseWriter, r *http.Request) {
		// Drain the finite upload before waiting. The HTTP server does not start
		// monitoring the connection for a client-side cancel while an unread
		// request body still owns that connection.
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(requestCanceled)
		case <-release:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := transcribeAudio(ctx, []byte("audio"), "a.wav", "")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("ASR request never reached server")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("cancellation err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("transcribeAudio did not return after cancellation")
	}
	select {
	case <-requestCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("server request context was not canceled")
	}
}

func TestTranscribeAudioConcurrentMultipartIsolation(t *testing.T) {
	const calls = 20
	var (
		mu       sync.Mutex
		captured = make(map[string]capturedASRRequest, calls)
	)
	asrTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got, err := captureASRRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		captured[got.filename] = got
		mu.Unlock()
		_, _ = io.WriteString(w, `{"transcription":"ok"}`)
	})

	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("audio-%02d.wav", i)
			payload := fmt.Sprintf("bytes-%02d", i)
			hotword := fmt.Sprintf("word-%02d", i)
			_, err := transcribeAudio(context.Background(), []byte(payload), name, hotword)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent transcribe: %v", err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != calls {
		t.Fatalf("captured %d requests, want %d", len(captured), calls)
	}
	for i := 0; i < calls; i++ {
		name := fmt.Sprintf("audio-%02d.wav", i)
		got := captured[name]
		if got.audio != fmt.Sprintf("bytes-%02d", i) || got.hotwords != fmt.Sprintf("word-%02d", i) {
			t.Fatalf("cross-request multipart contamination for %s: %+v", name, got)
		}
	}
}

func TestTranscribeAudioTextMergesBiasAndFormats(t *testing.T) {
	t.Setenv("DENEB_ASR_HOTWORDS", "operator-one, operator-two")
	captured := make(chan capturedASRRequest, 1)
	asrTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got, err := captureASRRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		captured <- got
		_, _ = io.WriteString(w, `{"segments":[{"start":65,"speaker":0,"content":"완료"}]}`)
	})
	text, err := transcribeAudioText(context.Background(), []byte("m4a"), "audio/mp4", " caller-one ")
	if err != nil {
		t.Fatalf("transcribeAudioText: %v", err)
	}
	if text != "[01:05 화자1] 완료" {
		t.Fatalf("text = %q", text)
	}
	got := <-captured
	if got.filename != "audio.m4a" {
		t.Fatalf("filename = %q", got.filename)
	}
	if got.hotwords != "caller-one, operator-one, operator-two" {
		t.Fatalf("hotword merge = %q", got.hotwords)
	}
}

func TestMergeHotwordsAndAudioFilenameBoundary(t *testing.T) {
	if got := mergeHotwords("", "  alpha ", "\n", "beta,gamma"); got != "alpha, beta,gamma" {
		t.Fatalf("mergeHotwords = %q", got)
	}
	for _, tc := range []struct {
		mime string
		want string
	}{
		{mime: " AUDIO/MP4; codecs=aac ", want: "audio.m4a"},
		{mime: "audio/aac", want: "audio.m4a"},
		{mime: "audio/mpeg", want: "audio.mp3"},
		{mime: "audio/opus", want: "audio.oga"},
		{mime: "audio/x-wav", want: "audio.wav"},
		{mime: "audio/webm", want: "audio.webm"},
		{mime: "audio/flac", want: "audio.flac"},
		{mime: "application/octet-stream", want: "audio"},
	} {
		if got := audioFilename(tc.mime); got != tc.want {
			t.Errorf("audioFilename(%q) = %q, want %q", tc.mime, got, tc.want)
		}
	}
}
