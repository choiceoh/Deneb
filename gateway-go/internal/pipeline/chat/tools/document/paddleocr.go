package document

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coreparsing/htmlmd"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// PaddleOCR-VL is Deneb's primary OCR engine: a 0.9B vision-language model
// (ERNIE-4.5-0.3B + NaViT encoder) served by vLLM on the local GPU
// (scripts side: start-paddleocr-vl.sh, port 18011). It far outperforms
// tesseract on Korean business documents — tables, formulas, mixed numbers,
// stamps — at roughly 1s per page once warm. tesseract stays as the fallback
// for when the model server is unreachable, so OCR degrades gracefully.

const (
	// ocrVLDefaultURL is the local vLLM OpenAI-compatible endpoint base.
	ocrVLDefaultURL = "http://127.0.0.1:18011"
	// ocrVLModel matches --served-model-name in start-paddleocr-vl.sh.
	ocrVLModel = "paddleocr-vl"
	// ocrVLTimeout bounds a single page/image request. Warm calls finish in
	// ~1s; the generous ceiling absorbs the one-time cold CUDA-graph warmup
	// that happens only on a fresh server boot.
	ocrVLTimeout = 90 * time.Second
)

// ocrVLBaseURL returns the OCR server base URL, overridable via
// DENEB_OCR_VL_URL for tests or a non-default deployment.
func ocrVLBaseURL() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_OCR_VL_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return ocrVLDefaultURL
}

// OpenAI-compatible chat request/response shapes — just the fields PaddleOCR-VL
// needs. The model takes one image plus a task prompt and returns plain text.
type ocrChatRequest struct {
	Model       string           `json:"model"`
	Messages    []ocrChatMessage `json:"messages"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
}

type ocrChatMessage struct {
	Role    string           `json:"role"`
	Content []ocrContentPart `json:"content"`
}

type ocrContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *ocrImageURL `json:"image_url,omitempty"`
}

type ocrImageURL struct {
	URL string `json:"url"`
}

type ocrChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		// FinishReason distinguishes a completed read from one the token
		// budget cut off. "length" is the strongest degeneration signal there
		// is — a real page almost never needs the full budget — and it was
		// being thrown away while the guard tried to re-derive the same fact
		// from text shape (2026-07-27 sheet-music audit).
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// paddleOCR sends one image to PaddleOCR-VL and returns the recognized text.
// task selects the recognition mode: "OCR:" for full-page text,
// "Table Recognition:", "Formula Recognition:", or "Chart Recognition:".
func paddleOCR(ctx context.Context, img []byte, task string) (string, string, error) {
	if task == "" {
		task = "OCR:"
	}
	mime := http.DetectContentType(img)
	if !strings.HasPrefix(mime, "image/") {
		mime = "image/png"
	}
	dataURI := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(img)

	reqBody, err := json.Marshal(ocrChatRequest{
		Model: ocrVLModel,
		Messages: []ocrChatMessage{{
			Role: "user",
			Content: []ocrContentPart{
				{Type: "image_url", ImageURL: &ocrImageURL{URL: dataURI}},
				{Type: "text", Text: task},
			},
		}},
		Temperature: 0,
		MaxTokens:   4096,
	})
	if err != nil {
		return "", "", err
	}

	runCtx, cancel := context.WithTimeout(ctx, ocrVLTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(runCtx, http.MethodPost,
		ocrVLBaseURL()+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httputil.NewClient(ocrVLTimeout).Do(req)
	if err != nil {
		return "", "", fmt.Errorf("paddleocr-vl 연결 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
		return "", "", fmt.Errorf("paddleocr-vl HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out ocrChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("paddleocr-vl 응답 파싱 실패: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", "", fmt.Errorf("paddleocr-vl 빈 응답")
	}
	text := strings.TrimSpace(out.Choices[0].Message.Content)
	if text == "" {
		return "", "", fmt.Errorf("paddleocr-vl 추출 텍스트 없음")
	}
	return text, out.Choices[0].FinishReason, nil
}

// ocrImageBytes is the single OCR entry point used across attachment handling.
// It prefers PaddleOCR-VL and falls back to tesseract when the local model
// server is unreachable or errors — connection refused fails instantly, so the
// fallback is cheap when the server is simply not running.
func ocrImageBytes(ctx context.Context, img []byte) (string, error) {
	// Repeat OCR of identical bytes (mail-poll analysis + chat open + re-asks)
	// is common — serve it from the content-addressed cache for free.
	if cached, ok := ocrCacheGet(img); ok {
		return cached, nil
	}
	text, finish, err := paddleOCR(ctx, img, "OCR:")
	if err == nil {
		final := ""
		// Dense pages trap the full-page mode in degenerate loops: item tables
		// repeat a row until max_tokens (2026-07-18 발주서, CER 2.58), and sheet
		// music loops on bar numbers and fingerings — "40" 1,020 times, "4 4 3
		// 2 1 2 3 2" 249 times (2026-07-27 audit). The old guard demanded ≥8
		// runes AND ≥4 letters per repeated line, so every number-loop slipped
		// through AND got cached, permanently serving garbage for that image.
		// ocrDegenerate folds all observed collapse shapes, including the
		// strongest signal of all: the token budget running out.
		if why := ocrDegenerate(text, finish); why != "" {
			slog.Default().Warn("paddleocr-vl full-page output degenerated; rescuing",
				"why", why, "chars", len(text))
			// Two rescues, best-of by surviving content: table mode re-reads
			// the page (saves dense tables and lyric pages), truncation keeps
			// the healthy prefix (saves headers when table mode loses them —
			// on the audited pages each rescue won once, so neither is enough
			// alone).
			var tableText string
			if t2, _, err2 := paddleOCR(ctx, img, "Table Recognition:"); err2 == nil {
				if conv := paddleTableToText(t2); ocrDegenerate(conv, "") == "" {
					tableText = strings.TrimSpace(conv)
				}
			}
			truncated := strings.TrimSpace(truncateAtLoop(text))
			if len(tableText) >= len(truncated) {
				final = tableText
			} else {
				final = truncated
			}
		}
		if final == "" {
			// PaddleOCR-VL's full-page mode recognizes tables and emits them
			// as HTML; render those as markdown so the model reads columns as
			// a grid instead of a flattened blob. No-op without a table.
			final = htmlTablesToMarkdown(text)
		}
		// Cache healthy results only: a degenerate last resort must stay
		// uncached so a later attempt gets to redo it.
		if ocrDegenerate(final, "") == "" {
			ocrCachePut(img, final)
		}
		return final, nil
	}
	slog.Default().Debug("paddleocr-vl unavailable, falling back to tesseract", "error", err)
	return tesseract(ctx, img)
}

// chartCachePayload namespaces chart-mode results in the content-addressed OCR
// cache: the cache key is a hash of the payload, and the same page image must
// not collide between full-page OCR text and chart-mode data.
func chartCachePayload(img []byte) []byte {
	return append([]byte("chart-recognition:"), img...)
}

// chartOCR asks PaddleOCR-VL's chart-recognition mode for the underlying data
// series of a chart image, normalized to readable rows. The output is a model
// ESTIMATE (bar heights become numbers), so callers must label it as such.
// Output that does not look like a data table is discarded — the mode can
// fabricate structure when pointed at a non-chart. No tesseract fallback:
// chart parsing needs the VL model.
func chartOCR(ctx context.Context, img []byte) (string, error) {
	if cached, ok := ocrCacheGet(chartCachePayload(img)); ok {
		return cached, nil
	}
	raw, _, err := paddleOCR(ctx, img, "Chart Recognition:")
	if err != nil {
		return "", err
	}
	table := strings.TrimSpace(paddleTableToText(raw))
	if !looksDataTable(table) {
		return "", fmt.Errorf("차트 데이터 형태 아님")
	}
	ocrCachePut(chartCachePayload(img), table)
	return table, nil
}

// looksDataTable reports whether s is a plausible multi-row data table —
// at least two non-empty rows carrying a column separator.
func looksDataTable(s string) bool {
	rows := 0
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" && strings.Contains(ln, "|") {
			rows++
			if rows >= 2 {
				return true
			}
		}
	}
	return false
}

func ocrDegenerate(text, finishReason string) string {
	// Budget exhaustion is degeneration by itself: a real page almost never
	// needs the whole 4096-token budget, and both audited collapses ended
	// exactly there.
	if finishReason == "length" {
		return "token-exhaustion"
	}
	lines := make([]string, 0, 64)
	for _, l := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	// Two repeat rules with different floors, because honest dense tables
	// flatten to one cell per line: "EA" or an amount column legitimately
	// recurs dozens of times INTERLEAVED with varying item lines. What honest
	// output never does is emit the same line ≥12 times CONSECUTIVELY — that
	// interleaving is the discriminator (the pre-existing honest-table tests
	// pinned it, and caught this detector's first draft over-firing).
	//
	// R1 — prose-line global count (the old guard, kept): a line with real
	// text repeating ≥12× anywhere is the 발주서 row-block collapse.
	counts := map[string]int{}
	for _, t := range lines {
		letters := 0
		for _, r := range t {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if len([]rune(t)) < 8 || letters < 4 {
			continue
		}
		counts[t]++
		if counts[t] >= 12 {
			return "line-repeat"
		}
	}
	// R2 — consecutive streak, NO floors: bar numbers and fingering rows
	// ("40" ×1020, "4 4 3 2 1 2 3 2" ×249) repeat back-to-back, which no
	// flattened honest column does.
	streakLine, streak := "", 0
	for _, t := range lines {
		if t == streakLine {
			streak++
			if streak >= 12 {
				return "line-streak"
			}
		} else {
			streakLine, streak = t, 1
		}
	}
	// Short repeating cycles (low-resolution loops emit "3","2","1","3","2",
	// "1",…): the same ≤8-line block from the head, ≥12 consecutive times.
	for period := 1; period <= 8; period++ {
		if len(lines) < period*12 {
			continue
		}
		matches := 0
		for i := 0; i+period <= len(lines); i += period {
			same := true
			for j := 0; j < period; j++ {
				if lines[i+j] != lines[j] {
					same = false
					break
				}
			}
			if !same {
				break
			}
			matches++
		}
		if matches >= 12 {
			return "cycle"
		}
	}
	// A single NON-WHITESPACE character ≥40× in a row (glyph smear: "♦♦♦…").
	// Whitespace must not count — healthy pages carry long alignment-space
	// runs (the carol page false-fired on exactly that offline).
	var prev rune
	run := 0
	for _, r := range text {
		if r == prev && !unicode.IsSpace(r) {
			run++
			if run >= 40 {
				return "char-run"
			}
		} else {
			run = 1
		}
		prev = r
	}
	return ""
}

// truncateAtLoop cuts the output where it collapses — the first line repeated
// six times consecutively — keeping the healthy prefix (title, composer,
// headers) that table-mode rescue loses on some pages.
func truncateAtLoop(text string) string {
	lines := strings.Split(text, "\n")
	last, streak := "", 0
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if t == last {
			streak++
			if streak >= 6 {
				return strings.Join(lines[:i-streak+1], "\n")
			}
		} else {
			last, streak = t, 1
		}
	}
	return text
}

// paddleTableToText renders a Table Recognition response as readable rows.
// That mode answers in PaddleOCR cell markup — <fcel>/<ecel> open cells,
// <lcel>/<ucel>/<xcel> continue merged cells, <nl> breaks rows — or plain
// HTML tables depending on content; both normalize to pipe-joined lines.
func paddleTableToText(s string) string {
	if strings.Contains(strings.ToLower(s), "<table") {
		return htmlTablesToMarkdown(s)
	}
	if !strings.Contains(s, "<fcel>") && !strings.Contains(s, "<nl>") {
		return s
	}
	var rows []string
	for _, row := range strings.Split(s, "<nl>") {
		// Merged-cell continuations carry no text of their own — drop the
		// markers, then split cells on the openers.
		for _, cont := range []string{"<lcel>", "<ucel>", "<xcel>"} {
			row = strings.ReplaceAll(row, cont, "")
		}
		row = strings.ReplaceAll(row, "<ecel>", "<fcel>")
		cells := []string{}
		for _, c := range strings.Split(row, "<fcel>") {
			if t := strings.TrimSpace(c); t != "" {
				cells = append(cells, t)
			}
		}
		if len(cells) > 0 {
			rows = append(rows, strings.Join(cells, " | "))
		}
	}
	return strings.Join(rows, "\n")
}

// htmlTablesToMarkdown converts any <table>…</table> blocks embedded in s to
// markdown, leaving the surrounding text untouched. PaddleOCR-VL returns
// recognized tables as HTML inside otherwise-plain OCR text; this normalizes
// them to GitHub-flavored markdown tables. A no-op when s contains no table.
func htmlTablesToMarkdown(s string) string {
	const openTag, closeTag = "<table", "</table>"
	lower := strings.ToLower(s)
	if !strings.Contains(lower, openTag) {
		return s
	}
	var b strings.Builder
	i := 0
	for {
		rel := strings.Index(lower[i:], openTag)
		if rel < 0 {
			b.WriteString(s[i:])
			break
		}
		start := i + rel
		endRel := strings.Index(lower[start:], closeTag)
		if endRel < 0 {
			b.WriteString(s[i:]) // unterminated table — keep the rest verbatim
			break
		}
		end := start + endRel + len(closeTag)
		b.WriteString(s[i:start]) // text before the table
		if md := strings.TrimSpace(htmlmd.Convert(s[start:end]).Text); md != "" {
			b.WriteString(md)
		} else {
			b.WriteString(s[start:end]) // conversion produced nothing — keep original
		}
		i = end
	}
	return b.String()
}
