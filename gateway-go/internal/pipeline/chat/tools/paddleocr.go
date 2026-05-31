package tools

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
	} `json:"choices"`
}

// paddleOCR sends one image to PaddleOCR-VL and returns the recognized text.
// task selects the recognition mode: "OCR:" for full-page text,
// "Table Recognition:", "Formula Recognition:", or "Chart Recognition:".
func paddleOCR(ctx context.Context, img []byte, task string) (string, error) {
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
		return "", err
	}

	runCtx, cancel := context.WithTimeout(ctx, ocrVLTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(runCtx, http.MethodPost,
		ocrVLBaseURL()+"/v1/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httputil.NewClient(ocrVLTimeout).Do(req)
	if err != nil {
		return "", fmt.Errorf("paddleocr-vl 연결 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024))
		return "", fmt.Errorf("paddleocr-vl HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out ocrChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("paddleocr-vl 응답 파싱 실패: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("paddleocr-vl 빈 응답")
	}
	text := strings.TrimSpace(out.Choices[0].Message.Content)
	if text == "" {
		return "", fmt.Errorf("paddleocr-vl 추출 텍스트 없음")
	}
	return text, nil
}

// ocrImageBytes is the single OCR entry point used across attachment handling.
// It prefers PaddleOCR-VL and falls back to tesseract when the local model
// server is unreachable or errors — connection refused fails instantly, so the
// fallback is cheap when the server is simply not running.
func ocrImageBytes(ctx context.Context, img []byte) (string, error) {
	text, err := paddleOCR(ctx, img, "OCR:")
	if err == nil {
		return text, nil
	}
	slog.Default().Debug("paddleocr-vl unavailable, falling back to tesseract", "error", err)
	return tesseract(ctx, img)
}
