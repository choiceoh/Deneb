package translateops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type deepLRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn deepLRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func deepLTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
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

func TestTranslateBatchDeepL_ReturnsTranslatedTextAndParts(t *testing.T) {
	resetTranslateTextCache()
	oldClient := deeplHTTPClient
	defer func() { deeplHTTPClient = oldClient }()

	var sawTarget string
	var sawTexts []string
	var sawContext string
	deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.String(); got != defaultDeepLTranslateURL {
			t.Errorf("url=%q want %q", got, defaultDeepLTranslateURL)
			return deepLTestResponse(http.StatusBadRequest, `{"message":"bad url"}`), nil
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%s want POST", r.Method)
			return deepLTestResponse(http.StatusMethodNotAllowed, `{"message":"bad method"}`), nil
		}
		if got := r.Header.Get("Authorization"); got != "DeepL-Auth-Key test-key" {
			t.Errorf("authorization=%q", got)
			return deepLTestResponse(http.StatusUnauthorized, `{"message":"bad auth"}`), nil
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Errorf("content-type=%q", got)
			return deepLTestResponse(http.StatusBadRequest, `{"message":"bad content type"}`), nil
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			return deepLTestResponse(http.StatusBadRequest, `{"message":"bad form"}`), nil
		}
		sawTarget = r.Form.Get("target_lang")
		sawTexts = append([]string(nil), r.Form["text"]...)
		sawContext = r.Form.Get("context")
		body, err := json.Marshal(map[string]any{
			"translations": []map[string]string{
				{"text": "안녕 "},
				{"text": "세계"},
				{"text": "더 보기"},
			},
		})
		if err != nil {
			t.Errorf("marshal response: %v", err)
			return deepLTestResponse(http.StatusInternalServerError, `{"message":"marshal"}`), nil
		}
		return deepLTestResponse(http.StatusOK, string(body)), nil
	})}
	t.Setenv("DEEPL_API_KEY", "test-key")

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
	resetTranslateTextCache()
	t.Setenv("DEEPL_API_KEY", "")
	out, ok := translateBatchDeepL(context.Background(), []translateInput{{Text: "Hello"}}, "ko")
	if ok || out != nil {
		t.Fatalf("out=%v ok=%v, want nil,false", out, ok)
	}
}

func TestTranslateBatchDeepLMismatchTriggersFallback(t *testing.T) {
	resetTranslateTextCache()
	oldClient := deeplHTTPClient
	defer func() { deeplHTTPClient = oldClient }()

	deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := json.Marshal(map[string]any{
			"translations": []map[string]string{{"text": "하나뿐"}},
		})
		if err != nil {
			t.Errorf("marshal response: %v", err)
			return deepLTestResponse(http.StatusInternalServerError, `{"message":"marshal"}`), nil
		}
		return deepLTestResponse(http.StatusOK, string(body)), nil
	})}
	t.Setenv("DEEPL_API_KEY", "test-key")

	out, ok := translateBatchDeepL(context.Background(), []translateInput{
		{Text: "Hello"},
		{Text: "World"},
	}, "Korean")
	if ok || out != nil {
		t.Fatalf("out=%v ok=%v, want nil,false", out, ok)
	}
}

func TestTranslateBatchDeepL_UsesServerCacheOnSecondCall(t *testing.T) {
	resetTranslateTextCache()
	oldClient := deeplHTTPClient
	defer func() { deeplHTTPClient = oldClient }()

	calls := 0
	deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return deepLTestResponse(http.StatusOK, `{"translations":[{"text":"안녕"}]}`), nil
	})}
	t.Setenv("DEEPL_API_KEY", "test-key")

	first, ok := translateBatchDeepL(context.Background(), []translateInput{{Text: "Hello"}}, "ko")
	if !ok || len(first) != 1 || first[0] != "안녕" {
		t.Fatalf("first out=%v ok=%v", first, ok)
	}
	second, ok := translateBatchDeepL(context.Background(), []translateInput{{Text: "Hello"}}, "ko")
	if !ok || len(second) != 1 || second[0] != "안녕" {
		t.Fatalf("second out=%v ok=%v", second, ok)
	}
	if calls != 1 {
		t.Fatalf("DeepL calls=%d want 1 (second served from server cache)", calls)
	}
}

func TestTranslateBatchDeepL_SingleflightCollapsesConcurrentMisses(t *testing.T) {
	resetTranslateTextCache()
	oldClient := deeplHTTPClient
	defer func() { deeplHTTPClient = oldClient }()

	var mu sync.Mutex
	calls := 0
	var startOnce sync.Once
	started := make(chan struct{})
	release := make(chan struct{})
	deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		startOnce.Do(func() { close(started) })
		<-release
		return deepLTestResponse(http.StatusOK, `{"translations":[{"text":"세계"}]}`), nil
	})}
	t.Setenv("DEEPL_API_KEY", "test-key")

	const n = 8
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, ok := translateBatchDeepL(context.Background(), []translateInput{{Text: "World"}}, "ko")
			if !ok || len(out) != 1 || out[0] != "세계" {
				errs <- fmt.Sprintf("out=%v ok=%v", out, ok)
			}
		}()
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for DeepL call")
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	mu.Lock()
	got := calls
	mu.Unlock()
	if got != 1 {
		t.Fatalf("DeepL calls=%d want 1 (singleflight)", got)
	}
}

func TestTranslateBatchDeepL_PartialCacheSkipsCachedTexts(t *testing.T) {
	resetTranslateTextCache()
	oldClient := deeplHTTPClient
	defer func() { deeplHTTPClient = oldClient }()

	rememberTranslated("KO", "Hello", "안녕")
	var sawTexts []string
	deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
			return deepLTestResponse(http.StatusBadRequest, `{}`), nil
		}
		sawTexts = append([]string(nil), r.Form["text"]...)
		return deepLTestResponse(http.StatusOK, `{"translations":[{"text":"세계"}]}`), nil
	})}
	t.Setenv("DEEPL_API_KEY", "test-key")

	out, ok := translateBatchDeepL(context.Background(), []translateInput{
		{Text: "Hello"},
		{Text: "World"},
	}, "ko")
	if !ok {
		t.Fatal("translateBatchDeepL returned !ok")
	}
	if got, want := strings.Join(sawTexts, "|"), "World"; got != want {
		t.Fatalf("DeepL texts=%q want only miss %q", got, want)
	}
	if len(out) != 2 || out[0] != "안녕" || out[1] != "세계" {
		t.Fatalf("out=%v", out)
	}
}

func TestDeepLTranslateEndpointReturnsValidatedURL(t *testing.T) {
	t.Setenv("DEEPL_API_URL", "")
	if got := deepLTranslateEndpoint(); got != defaultDeepLTranslateURL {
		t.Fatalf("default endpoint=%q want %q", got, defaultDeepLTranslateURL)
	}

	t.Setenv("DEEPL_API_URL", "https://api-free.deepl.com/v2/translate")
	if got := deepLTranslateEndpoint(); got != "https://api-free.deepl.com/v2/translate" {
		t.Fatalf("free endpoint=%q", got)
	}

	for _, bad := range []string{
		"http://api.deepl.com/v2/translate",
		"https://127.0.0.1/v2/translate",
		"https://api.deepl.com/admin",
		"https://api.deepl.com/v2/translate?x=1",
	} {
		t.Setenv("DEEPL_API_URL", bad)
		if got := deepLTranslateEndpoint(); got != "" {
			t.Fatalf("bad endpoint %q returned %q", bad, got)
		}
	}
}

func TestDeepLTargetLangReturnsMappedCode(t *testing.T) {
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
