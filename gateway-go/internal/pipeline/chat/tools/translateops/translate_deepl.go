package translateops

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	denhttp "github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const (
	defaultDeepLTranslateURL = "https://api.deepl.com/v2/translate"
	deeplTimeout             = 20 * time.Second
	maxDeepLContextChars     = 1800
	maxDeepLTextsPerRequest  = 50
	// A 429 (rate limit) or 5xx is worth ONE bounded retry. Without it the batch
	// reports failure, translateRange splits it and retries the halves — each half
	// hitting the same limit — and a page ends up with untranslated originals after
	// spending MORE calls than it started with. Retry-After is honoured up to the cap.
	deeplRetryAttempts = 2
	deeplRetryWaitCap  = 3 * time.Second
)

type deepLFlatItem struct {
	segment int
	part    int
}

type deepLTranslationResponse struct {
	Translations []struct {
		Text string `json:"text"`
	} `json:"translations"`
}

var (
	deeplHTTPClient = denhttp.NewClient(deeplTimeout)
	deeplLangCodeRE = regexp.MustCompile(`^[A-Za-z]{2}(?:[-_][A-Za-z]{2,4})?$`)
)

func translateBatchDeepL(ctx context.Context, batch []translateInput, lang string) ([]string, translateBatchOutcome) {
	// These three are properties of the deployment, not of this batch: no amount
	// of splitting the range produces a key, an endpoint or a supported language.
	key := strings.TrimSpace(os.Getenv("DEEPL_API_KEY"))
	if key == "" {
		return nil, batchHopeless
	}
	target := deepLTargetLang(lang)
	if target == "" {
		return nil, batchHopeless
	}
	endpoint := deepLTranslateEndpoint()
	if endpoint == "" {
		return nil, batchHopeless
	}

	out, partOut, texts, mapping := flattenDeepLInputs(batch)
	if len(texts) == 0 {
		if encodeDeepLParts(out, partOut) {
			return out, batchOK
		}
		return nil, batchRetryable
	}
	// Too many flattened texts for one request — splitting is exactly the remedy.
	if len(texts) > maxDeepLTextsPerRequest {
		return nil, batchRetryable
	}

	resolved := make([]string, len(texts))
	missIdx := make([]int, 0, len(texts))
	missTexts := make([]string, 0, len(texts))
	for i, text := range texts {
		if cached, ok := translateCached(target, text); ok {
			resolved[i] = cached
			continue
		}
		missIdx = append(missIdx, i)
		missTexts = append(missTexts, text)
	}
	if len(missTexts) > 0 {
		// The singleflight leader reports its verdict out here: followers share
		// the leader's result, so they must share its "stop splitting" too.
		var hopeless atomic.Bool
		contextHint := deepLContext(batch)
		flightKey := translateMissFlightKey(target, missTexts)
		translated, ok := translateFlight.do(flightKey, func() ([]string, bool) {
			// Re-check cache inside the flight leader: a just-finished sibling
			// batch may have filled every miss while we waited to become leader.
			stillMissIdx := make([]int, 0, len(missTexts))
			stillMiss := make([]string, 0, len(missTexts))
			filled := make([]string, len(missTexts))
			for i, text := range missTexts {
				if cached, hit := translateCached(target, text); hit {
					filled[i] = cached
					continue
				}
				stillMissIdx = append(stillMissIdx, i)
				stillMiss = append(stillMiss, text)
			}
			if len(stillMiss) == 0 {
				return filled, true
			}
			fresh, outcome := callDeepL(ctx, endpoint, key, target, stillMiss, contextHint)
			if outcome != batchOK || len(fresh) != len(stillMiss) {
				if outcome == batchHopeless {
					hopeless.Store(true)
				}
				return nil, false
			}
			for i, text := range fresh {
				filled[stillMissIdx[i]] = text
				rememberTranslated(target, stillMiss[i], text)
			}
			return filled, true
		})
		if !ok || len(translated) != len(missTexts) {
			if hopeless.Load() {
				return nil, batchHopeless
			}
			// Full DeepL miss → retryable so translateRange can bisect. Cache
			// hits still help on the smaller retries.
			return nil, batchRetryable
		}
		for i, text := range translated {
			resolved[missIdx[i]] = text
			rememberTranslated(target, missTexts[i], text)
		}
	}
	for i, text := range resolved {
		m := mapping[i]
		if m.part >= 0 {
			partOut[m.segment][m.part] = text
			continue
		}
		out[m.segment] = text
	}
	if !encodeDeepLParts(out, partOut) {
		return nil, batchRetryable
	}
	return out, batchOK
}

func encodeDeepLParts(out []string, partOut [][]string) bool {
	for i, parts := range partOut {
		if parts == nil {
			continue
		}
		encoded, err := json.Marshal(parts)
		if err != nil {
			return false
		}
		out[i] = translatePartsEnvelopePrefix + string(encoded)
	}
	return true
}

func flattenDeepLInputs(batch []translateInput) ([]string, [][]string, []string, []deepLFlatItem) {
	out := make([]string, len(batch))
	partOut := make([][]string, len(batch))
	var texts []string
	var mapping []deepLFlatItem
	for i, in := range batch {
		out[i] = in.Text
		if len(in.Parts) == 0 {
			if strings.TrimSpace(in.Text) == "" {
				continue
			}
			texts = append(texts, in.Text)
			mapping = append(mapping, deepLFlatItem{segment: i, part: -1})
			continue
		}
		partOut[i] = append([]string(nil), in.Parts...)
		for partIdx, part := range in.Parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			texts = append(texts, part)
			mapping = append(mapping, deepLFlatItem{segment: i, part: partIdx})
		}
	}
	return out, partOut, texts, mapping
}

func callDeepL(ctx context.Context, endpoint, key, target string, texts []string, contextHint string) ([]string, translateBatchOutcome) {
	form := url.Values{}
	for _, text := range texts {
		form.Add("text", text)
	}
	form.Set("target_lang", target)
	if contextHint != "" {
		form.Set("context", contextHint)
	}
	body := form.Encode()
	for attempt := 0; attempt < deeplRetryAttempts; attempt++ {
		out, status, ok := postDeepLOnce(ctx, endpoint, key, body)
		if ok {
			return out, batchOK
		}
		if attempt == deeplRetryAttempts-1 || !deepLStatusWorthRetry(status) {
			if deepLStatusSplittable(status) {
				return nil, batchRetryable
			}
			return nil, batchHopeless
		}
		if !sleepCtx(ctx, deepLRetryWait(status)) {
			return nil, batchHopeless // the caller's context ended
		}
	}
	return nil, batchHopeless
}

// deepLStatusSplittable: only a complaint about THIS request's payload can be
// answered by sending a smaller one, and that is the only reason translateRange
// splits a range. Auth (401/403), exhausted quota (456), rate limit (429),
// server error and transport failure are all account- or service-level — every
// half gets the identical answer, so splitting a 40-segment range into 79 calls
// only asks 79 times. An over-long flattened batch is caught before the request
// is even built (maxDeepLTextsPerRequest), which is the common legitimate split.
func deepLStatusSplittable(status int) bool {
	return status == http.StatusBadRequest ||
		status == http.StatusRequestEntityTooLarge ||
		status == http.StatusRequestURITooLong
}

// deepLStatusWorthRetry: 429 is the documented rate limit and 5xx is DeepL's own
// hiccup; every other failure (auth, quota 456, malformed request) repeats
// identically, so retrying it only burns time the caller does not have.
func deepLStatusWorthRetry(status int) bool {
	return status == http.StatusTooManyRequests || (status >= 500 && status <= 599)
}

func deepLRetryWait(status int) time.Duration {
	if status == http.StatusTooManyRequests {
		return deeplRetryWaitCap
	}
	return 500 * time.Millisecond
}

// sleepCtx reports whether the wait completed (false = the caller's context ended).
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// postDeepLOnce returns (translations, httpStatus, ok). status is 0 when the
// request never produced a response.
func postDeepLOnce(ctx context.Context, endpoint, key, body string) ([]string, int, bool) {
	//nolint:gosec // endpoint is restricted to official DeepL translate hosts by deepLTranslateEndpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, 0, false
	}
	req.Header.Set("Authorization", "DeepL-Auth-Key "+key)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	//nolint:gosec // req URL comes from the same DeepL-only endpoint gate above.
	resp, err := deeplHTTPClient.Do(req)
	if err != nil {
		return nil, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, resp.StatusCode, false
	}
	var payload deepLTranslationResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, resp.StatusCode, false
	}
	out := make([]string, len(payload.Translations))
	for i, tr := range payload.Translations {
		out[i] = tr.Text
	}
	return out, resp.StatusCode, true
}

func deepLTranslateEndpoint() string {
	endpoint := strings.TrimSpace(os.Getenv("DEEPL_API_URL"))
	if endpoint == "" {
		return defaultDeepLTranslateURL
	}
	if isDeepLTranslateEndpoint(endpoint) {
		return endpoint
	}
	return ""
}

func isDeepLTranslateEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "api.deepl.com", "api-free.deepl.com":
	default:
		return false
	}
	return strings.TrimRight(u.EscapedPath(), "/") == "/v2/translate"
}

func deepLContext(batch []translateInput) string {
	var b strings.Builder
	for _, in := range batch {
		addDeepLContextPart(&b, in.Role)
		addDeepLContextPart(&b, in.Context)
		if b.Len() >= maxDeepLContextChars {
			break
		}
	}
	if b.Len() > maxDeepLContextChars {
		runes := []rune(b.String())
		if len(runes) > maxDeepLContextChars {
			return string(runes[:maxDeepLContextChars])
		}
	}
	return strings.TrimSpace(b.String())
}

func addDeepLContextPart(b *strings.Builder, part string) {
	part = strings.TrimSpace(part)
	if part == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	b.WriteString(part)
}

func deepLTargetLang(lang string) string {
	normalized := strings.ToLower(strings.TrimSpace(lang))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "", "ko", "kor", "kr", "korean", "한국어":
		return "KO"
	case "en", "english":
		return "EN-US"
	case "en-us", "american english":
		return "EN-US"
	case "en-gb", "british english":
		return "EN-GB"
	case "ja", "jp", "japanese", "일본어":
		return "JA"
	case "zh", "zh-cn", "zh-hans", "chinese", "중국어":
		return "ZH-HANS"
	case "zh-tw", "zh-hant", "traditional chinese":
		return "ZH-HANT"
	case "de", "german":
		return "DE"
	case "fr", "french":
		return "FR"
	case "es", "spanish":
		return "ES"
	}
	if deeplLangCodeRE.MatchString(normalized) {
		return strings.ToUpper(normalized)
	}
	return ""
}
