package translateops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTranslateSegmentsUnicodeBudgetPreservesResults(t *testing.T) {
	for _, script := range []struct {
		name string
		char string
	}{
		{name: "Latin", char: "a"},
		{name: "Cyrillic", char: "я"},
		{name: "CJK", char: "界"},
		{name: "supplementary", char: "🛰"},
	} {
		t.Run(script.name, func(t *testing.T) {
			useTempDiskCache(t)
			t.Setenv("DEEPL_API_KEY", "test-key")
			t.Setenv("DEEPL_API_URL", defaultDeepLTranslateURL)
			oldClient := deeplHTTPClient
			t.Cleanup(func() { deeplHTTPClient = oldClient })
			var calls atomic.Int32
			deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.ContentLength > 128<<10 {
					return nil, fmt.Errorf("request exceeds DeepL's byte limit: %d", r.ContentLength)
				}
				if err := r.ParseForm(); err != nil {
					return nil, err
				}
				texts := r.Form["text"]
				if len(texts) > maxDeepLTextsPerRequest {
					return nil, fmt.Errorf("too many provider texts: %d", len(texts))
				}
				calls.Add(1)
				translations := make([]map[string]string, len(texts))
				for i, text := range texts {
					translations[i] = map[string]string{"text": "번역: " + text}
				}
				body, err := json.Marshal(map[string]any{"translations": translations})
				if err != nil {
					return nil, err
				}
				return deepLTestResponse(http.StatusOK, string(body)), nil
			})}
			// Forty 400-character segments fit in six calls at the existing
			// budget, regardless of how many bytes encode each character.
			segments := make([]string, 40)
			for i := range segments {
				segments[i] = fmt.Sprintf("%02d ", i) + strings.Repeat(script.char, 397)
			}
			out, err := TranslateSegments(context.Background(), segments, "ko")
			if err != nil || len(out) != len(segments) {
				t.Fatalf("TranslateSegments: count=%d, err=%v", len(out), err)
			}
			for i, text := range out {
				if text != "번역: "+segments[i] {
					t.Fatalf("segment %d lost or reordered", i)
				}
			}
			if got := calls.Load(); got != 6 {
				t.Fatalf("provider calls=%d, want 6", got)
			}
		})
	}
}
