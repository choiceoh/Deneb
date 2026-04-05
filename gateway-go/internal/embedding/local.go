package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ffi"
)

// LocalEmbedder generates embeddings via a local GGUF model through Rust FFI.
type LocalEmbedder struct {
	modelPath string
	modelName string
	logger    *slog.Logger
}

// NewLocalEmbedder creates an embedder that calls the Rust ML FFI.
// Returns nil if modelPath is empty.
func NewLocalEmbedder(modelPath string, logger *slog.Logger) *LocalEmbedder {
	if modelPath == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	name := strings.TrimSuffix(filepath.Base(modelPath), filepath.Ext(modelPath))
	return &LocalEmbedder{
		modelPath: modelPath,
		modelName: name,
		logger:    logger,
	}
}

// ModelName returns the short model identifier (e.g. "bge-m3-q4_k_m").
func (l *LocalEmbedder) ModelName() string {
	return l.modelName
}

// EmbedQuery embeds a single text and returns an L2-normalized float32 vector.
func (l *LocalEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vecs, err := l.embedViaFFI(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("local embed: empty result")
	}
	return vecs[0], nil
}

// EmbedBatch embeds multiple texts and returns L2-normalized vectors in order.
func (l *LocalEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return l.embedViaFFI(ctx, texts)
}

// mlEmbedRequest is the JSON input to deneb_ml_embed.
type mlEmbedRequest struct {
	Texts     []string `json:"texts"`
	ModelPath string   `json:"model_path"`
}

// mlEmbedResponse is the JSON output from deneb_ml_embed.
type mlEmbedResponse struct {
	Vectors [][]float32 `json:"vectors"`
	Dim     int         `json:"dim"`
	Model   string      `json:"model"`
	Error   string      `json:"error,omitempty"`
	Detail  string      `json:"detail,omitempty"`
}

func (l *LocalEmbedder) embedViaFFI(ctx context.Context, texts []string) ([][]float32, error) {
	req := mlEmbedRequest{
		Texts:     texts,
		ModelPath: l.modelPath,
	}

	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("local embed: marshal: %w", err)
	}

	respBytes, err := ffi.MLEmbedCtx(ctx, string(reqJSON))
	if err != nil {
		return nil, fmt.Errorf("local embed: ffi: %w", err)
	}

	var resp mlEmbedResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("local embed: unmarshal: %w", err)
	}

	if resp.Error != "" {
		detail := resp.Error
		if resp.Detail != "" {
			detail += ": " + resp.Detail
		}
		return nil, fmt.Errorf("local embed: %s", detail)
	}

	if len(resp.Vectors) != len(texts) {
		return nil, fmt.Errorf("local embed: expected %d vectors, got %d", len(texts), len(resp.Vectors))
	}

	return resp.Vectors, nil
}
