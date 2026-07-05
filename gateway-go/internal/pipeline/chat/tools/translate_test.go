package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseTranslations(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
		ok   bool
		out  []string
	}{
		{"clean array", `["안녕","세계"]`, 2, true, []string{"안녕", "세계"}},
		{"code fenced", "```json\n[\"하나\",\"둘\"]\n```", 2, true, []string{"하나", "둘"}},
		{"envelope", `{"translations":["가","나","다"]}`, 3, true, []string{"가", "나", "다"}},
		{"count short", `["하나"]`, 2, false, nil},
		{"count long", `["하나","둘","셋"]`, 2, false, nil},
		{"garbage", `not json at all`, 1, false, nil},
		{"empty array vs want", `[]`, 1, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTranslations(tc.raw, tc.want)
			if ok != tc.ok {
				t.Fatalf("ok=%v want %v (raw=%q)", ok, tc.ok, tc.raw)
			}
			if ok {
				if len(got) != len(tc.out) {
					t.Fatalf("len=%d want %d", len(got), len(tc.out))
				}
				for i := range got {
					if got[i] != tc.out[i] {
						t.Fatalf("got[%d]=%q want %q", i, got[i], tc.out[i])
					}
				}
			}
		})
	}
}

// TestTranslateSegments_EmptyAndCountInvariant checks the structural guarantees
// that don't need a live model: empty input is a no-op, and the function always
// returns a slice the SAME length as its input (the index-replacement contract).
func TestTranslateSegments_Empty(t *testing.T) {
	out, err := TranslateSegments(context.Background(), nil, "Korean")
	if err != nil || out != nil {
		t.Fatalf("empty input: got out=%v err=%v, want nil,nil", out, err)
	}
}

func TestBuildTranslatePrompt_AnchorsCountAndSegments(t *testing.T) {
	system, user := buildTranslatePrompt([]translateInput{
		{Text: "Hello"},
		{Text: "Привет", Context: "Привет мир", Role: "body"},
	}, "Korean")
	if system == "" || user == "" {
		t.Fatal("expected non-empty prompt")
	}
	// The user message must carry the exact count and the raw segments so the
	// model is anchored to return the same number of items.
	for _, want := range []string{"exactly 2", "Hello", "Привет", "Привет мир"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, user)
		}
	}
}

func TestTranslateSegments_EnvelopeFallbackKeepsOriginalText(t *testing.T) {
	old := translateBatchFn
	translateBatchFn = func(context.Context, []translateInput, string) ([]string, bool) {
		return nil, false
	}
	defer func() { translateBatchFn = old }()

	raw := translateSegmentEnvelopePrefix + `{"text":"Hello","context":"Hello world","role":"body"}`
	out, err := TranslateSegments(context.Background(), []string{raw}, "Korean")
	if err != nil {
		t.Fatalf("TranslateSegments error: %v", err)
	}
	if len(out) != 1 || out[0] != "Hello" {
		t.Fatalf("fallback out=%q, want original text only", out)
	}
}

func TestParseTranslationItems_GroupedParts(t *testing.T) {
	inputs := []translateInput{
		{Text: "Hello world", Parts: []string{"Hello ", "world"}},
		{Text: "Read more"},
	}
	out, ok := parseTranslationItems(`[["안녕 ","세계"],"더 보기"]`, inputs)
	if !ok {
		t.Fatal("parseTranslationItems returned !ok")
	}
	wantParts := translatePartsEnvelopePrefix + `["안녕 ","세계"]`
	if len(out) != 2 || out[0] != wantParts || out[1] != "더 보기" {
		t.Fatalf("out=%q want [%q %q]", out, wantParts, "더 보기")
	}
}

func TestTranslateSegments_TranslatesBatchesConcurrently(t *testing.T) {
	old := translateBatchFn
	defer func() { translateBatchFn = old }()

	var mu sync.Mutex
	inFlight := 0
	maxInFlight := 0
	calls := 0
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	translateBatchFn = func(ctx context.Context, batch []translateInput, lang string) ([]string, bool) {
		mu.Lock()
		calls++
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		mu.Unlock()
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, false
		}
		out := make([]string, len(batch))
		for i, in := range batch {
			out[i] = "ko:" + in.Text
		}
		mu.Lock()
		inFlight--
		mu.Unlock()
		return out, true
	}

	segments := make([]string, 5)
	for i := range segments {
		segments[i] = strings.Repeat(fmt.Sprintf("%d", i), 900)
	}

	done := make(chan struct{})
	var out []string
	var err error
	go func() {
		out, err = TranslateSegments(context.Background(), segments, "Korean")
		close(done)
	}()

	for i := 0; i < translateMaxConcurrentBatches; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for concurrent batch %d", i+1)
		}
	}

	mu.Lock()
	gotMax := maxInFlight
	mu.Unlock()
	if gotMax != translateMaxConcurrentBatches {
		close(release)
		t.Fatalf("max concurrency=%d want %d", gotMax, translateMaxConcurrentBatches)
	}
	close(release)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("TranslateSegments did not finish")
	}
	if err != nil {
		t.Fatalf("TranslateSegments error: %v", err)
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != len(segments) {
		t.Fatalf("translate calls=%d want %d", gotCalls, len(segments))
	}
	for i := range segments {
		if want := "ko:" + segments[i]; out[i] != want {
			t.Fatalf("out[%d]=%q want %q", i, out[i], want)
		}
	}
}

func TestTranslateBatchDeepL_TextAndParts(t *testing.T) {
	oldClient := deeplHTTPClient
	defer func() { deeplHTTPClient = oldClient }()

	var sawTarget string
	var sawTexts []string
	var sawContext string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s want POST", r.Method)
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if got := r.Header.Get("Authorization"); got != "DeepL-Auth-Key test-key" {
			t.Errorf("authorization=%q", got)
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Errorf("content-type=%q", got)
			http.Error(w, "bad content type", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		sawTarget = r.Form.Get("target_lang")
		sawTexts = append([]string(nil), r.Form["text"]...)
		sawContext = r.Form.Get("context")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"translations": []map[string]string{
				{"text": "안녕 "},
				{"text": "세계"},
				{"text": "더 보기"},
			},
		})
	}))
	defer srv.Close()
	deeplHTTPClient = srv.Client()
	t.Setenv("DEEPL_API_KEY", "test-key")
	t.Setenv("DEEPL_API_URL", srv.URL)

	out, ok := translateBatchDeepL(context.Background(), []translateInput{
		{Text: "Hello world", Parts: []string{"Hello ", "world"}, Context: "Greeting block", Role: "body"},
		{Text: "Read more"},
	}, "ko")
	if !ok {
		t.Fatal("translateBatchDeepL returned !ok")
	}
	if sawTarget != "KO" {
		t.Fatalf("target_lang=%q want KO", sawTarget)
	}
	if got, want := strings.Join(sawTexts, "|"), "Hello |world|Read more"; got != want {
		t.Fatalf("texts=%q want %q", got, want)
	}
	if !strings.Contains(sawContext, "Greeting block") || !strings.Contains(sawContext, "body") {
		t.Fatalf("context=%q missing role/context", sawContext)
	}
	wantParts := translatePartsEnvelopePrefix + `["안녕 ","세계"]`
	if len(out) != 2 || out[0] != wantParts || out[1] != "더 보기" {
		t.Fatalf("out=%q want [%q %q]", out, wantParts, "더 보기")
	}
}

func TestTranslateBatchDeepLDisabledWithoutKey(t *testing.T) {
	t.Setenv("DEEPL_API_KEY", "")
	out, ok := translateBatchDeepL(context.Background(), []translateInput{{Text: "Hello"}}, "ko")
	if ok || out != nil {
		t.Fatalf("out=%v ok=%v, want nil,false", out, ok)
	}
}

func TestTranslateBatchDeepLMismatchFallsBack(t *testing.T) {
	oldClient := deeplHTTPClient
	defer func() { deeplHTTPClient = oldClient }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"translations": []map[string]string{{"text": "하나뿐"}},
		})
	}))
	defer srv.Close()
	deeplHTTPClient = srv.Client()
	t.Setenv("DEEPL_API_KEY", "test-key")
	t.Setenv("DEEPL_API_URL", srv.URL)

	out, ok := translateBatchDeepL(context.Background(), []translateInput{
		{Text: "Hello"},
		{Text: "World"},
	}, "Korean")
	if ok || out != nil {
		t.Fatalf("out=%v ok=%v, want nil,false", out, ok)
	}
}

func TestDeepLTargetLang(t *testing.T) {
	cases := map[string]string{
		"":                  "KO",
		"ko":                "KO",
		"Korean":            "KO",
		"en":                "EN-US",
		"British English":   "EN-GB",
		"zh-Hant":           "ZH-HANT",
		"pt-br":             "PT-BR",
		"not a language id": "",
	}
	for in, want := range cases {
		if got := deepLTargetLang(in); got != want {
			t.Fatalf("deepLTargetLang(%q)=%q want %q", in, got, want)
		}
	}
}
