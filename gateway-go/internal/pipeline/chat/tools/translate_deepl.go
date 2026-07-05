package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	denhttp "github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

const (
	defaultDeepLTranslateURL = "https://api.deepl.com/v2/translate"
	deeplTimeout             = 20 * time.Second
	maxDeepLContextChars     = 1800
	maxDeepLTextsPerRequest  = 50
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

func translateBatchDeepL(ctx context.Context, batch []translateInput, lang string) ([]string, bool) {
	key := strings.TrimSpace(os.Getenv("DEEPL_API_KEY"))
	if key == "" {
		return nil, false
	}
	target := deepLTargetLang(lang)
	if target == "" {
		return nil, false
	}
	endpoint := deepLTranslateEndpoint()
	if endpoint == "" {
		return nil, false
	}

	out, partOut, texts, mapping := flattenDeepLInputs(batch)
	if len(texts) == 0 {
		return out, encodeDeepLParts(out, partOut)
	}
	if len(texts) > maxDeepLTextsPerRequest {
		return nil, false
	}

	translated, ok := callDeepL(ctx, endpoint, key, target, texts, deepLContext(batch))
	if !ok || len(translated) != len(texts) {
		return nil, false
	}
	for i, text := range translated {
		m := mapping[i]
		if m.part >= 0 {
			partOut[m.segment][m.part] = text
			continue
		}
		out[m.segment] = text
	}
	if !encodeDeepLParts(out, partOut) {
		return nil, false
	}
	return out, true
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

func callDeepL(ctx context.Context, endpoint, key, target string, texts []string, contextHint string) ([]string, bool) {
	form := url.Values{}
	for _, text := range texts {
		form.Add("text", text)
	}
	form.Set("target_lang", target)
	if contextHint != "" {
		form.Set("context", contextHint)
	}
	//nolint:gosec // endpoint is restricted to official DeepL translate hosts by deepLTranslateEndpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "DeepL-Auth-Key "+key)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	//nolint:gosec // req URL comes from the same DeepL-only endpoint gate above.
	resp, err := deeplHTTPClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return nil, false
	}
	var payload deepLTranslationResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, false
	}
	out := make([]string, len(payload.Translations))
	for i, tr := range payload.Translations {
		out[i] = tr.Text
	}
	return out, true
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
