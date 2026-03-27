// Package embedding provides embedding clients for vector search and memory.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"
)

const (
	geminiModel   = "gemini-embedding-2-preview"
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models/" + geminiModel

	embedTimeout = 30 * time.Second
	// Maximum texts per batchEmbedContents call (Gemini limit is 100).
	maxBatchSize = 100
)

// GeminiEmbedder generates embeddings via Google's Gemini Embedding API.
type GeminiEmbedder struct {
	apiKey string
	client *http.Client
	logger *slog.Logger
}

// NewGeminiEmbedder creates an embedder that calls the Gemini Embedding API.
// Returns nil if apiKey is empty.
func NewGeminiEmbedder(apiKey string, logger *slog.Logger) *GeminiEmbedder {
	if apiKey == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &GeminiEmbedder{
		apiKey: apiKey,
		client: &http.Client{Timeout: embedTimeout},
		logger: logger,
	}
}

// EmbedQuery embeds a single text and returns an L2-normalized float32 vector.
func (g *GeminiEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	req := geminiEmbedRequest{
		Model: "models/" + geminiModel,
		Content: geminiContent{
			Parts: []geminiPart{{Text: text}},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embed: marshal: %w", err)
	}

	url := geminiBaseURL + ":embedContent"
	respBody, err := g.doRequest(ctx, url, body)
	if err != nil {
		return nil, fmt.Errorf("gemini embed: %w", err)
	}
	defer respBody.Close()

	data, err := io.ReadAll(io.LimitReader(respBody, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("gemini embed: read response: %w", err)
	}

	var resp geminiEmbedResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("gemini embed: unmarshal: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("gemini embed: API error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	vec := toFloat32(resp.Embedding.Values)
	l2Normalize(vec)
	return vec, nil
}

// EmbedBatch embeds multiple texts and returns L2-normalized vectors in order.
func (g *GeminiEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Split into chunks if exceeding max batch size.
	if len(texts) > maxBatchSize {
		var all [][]float32
		for i := 0; i < len(texts); i += maxBatchSize {
			end := i + maxBatchSize
			if end > len(texts) {
				end = len(texts)
			}
			batch, err := g.embedBatchChunk(ctx, texts[i:end])
			if err != nil {
				return nil, err
			}
			all = append(all, batch...)
		}
		return all, nil
	}

	return g.embedBatchChunk(ctx, texts)
}

func (g *GeminiEmbedder) embedBatchChunk(ctx context.Context, texts []string) ([][]float32, error) {
	requests := make([]geminiEmbedRequest, len(texts))
	for i, t := range texts {
		requests[i] = geminiEmbedRequest{
			Model: "models/" + geminiModel,
			Content: geminiContent{
				Parts: []geminiPart{{Text: t}},
			},
		}
	}

	body, err := json.Marshal(geminiBatchRequest{Requests: requests})
	if err != nil {
		return nil, fmt.Errorf("gemini batch embed: marshal: %w", err)
	}

	url := geminiBaseURL + ":batchEmbedContents"
	respBody, err := g.doRequest(ctx, url, body)
	if err != nil {
		return nil, fmt.Errorf("gemini batch embed: %w", err)
	}
	defer respBody.Close()

	data, err := io.ReadAll(io.LimitReader(respBody, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("gemini batch embed: read response: %w", err)
	}

	var resp geminiBatchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("gemini batch embed: unmarshal: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("gemini batch embed: API error %d: %s", resp.Error.Code, resp.Error.Message)
	}

	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini batch embed: expected %d embeddings, got %d", len(texts), len(resp.Embeddings))
	}

	result := make([][]float32, len(texts))
	for i, emb := range resp.Embeddings {
		vec := toFloat32(emb.Values)
		l2Normalize(vec)
		result[i] = vec
	}
	return result, nil
}

// doRequest sends a POST request to the Gemini API.
func (g *GeminiEmbedder) doRequest(ctx context.Context, url string, body []byte) (io.ReadCloser, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", g.apiKey)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(errBody))
	}

	return resp.Body, nil
}

// --- Gemini API types ---

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiEmbedRequest struct {
	Model   string        `json:"model"`
	Content geminiContent `json:"content"`
}

type geminiEmbedResponse struct {
	Embedding geminiEmbedding `json:"embedding"`
	Error     *geminiError    `json:"error,omitempty"`
}

type geminiEmbedding struct {
	Values []float64 `json:"values"`
}

type geminiBatchRequest struct {
	Requests []geminiEmbedRequest `json:"requests"`
}

type geminiBatchResponse struct {
	Embeddings []geminiEmbedding `json:"embeddings"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// --- Helpers ---

// toFloat32 converts float64 slice to float32.
func toFloat32(f64 []float64) []float32 {
	f32 := make([]float32, len(f64))
	for i, v := range f64 {
		f32[i] = float32(v)
	}
	return f32
}

// l2Normalize normalizes a vector in-place to unit length.
func l2Normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
}
