package localai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

// cjkBlock holds pre-serialized logit_bias bytes for blocking CJK tokens.
// Loaded and serialized once at hub startup; injected into every local AI request
// as json.RawMessage to avoid re-marshaling 55K entries per request.
type cjkBlock struct {
	// raw is the pre-serialized logit_bias JSON: {"1234":-100,"5678":-100,...}.
	// Stored as json.RawMessage so mergeJSONFields passes it through as-is.
	raw json.RawMessage

	// count is the number of blocked tokens (for logging).
	count int
}

// cjkTokenFile is the JSON format output by block_cjk.py scan.
type cjkTokenFile struct {
	Model           string `json:"model"`
	VocabSize       int    `json:"vocab_size"`
	BlockedCount    int    `json:"blocked_count"`
	KoreanCount     int    `json:"korean_count"`
	MixedCount      int    `json:"mixed_count"`
	BlockedTokenIDs []int  `json:"blocked_token_ids"`
}

// loadCJKBlock loads pre-computed CJK token IDs from a JSON file, builds the
// logit_bias map, and pre-serializes it to JSON bytes once. Returns nil if
// path is empty (feature disabled).
func loadCJKBlock(path string, logger *slog.Logger) *cjkBlock {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		logger.Error("failed to load CJK block file", "path", path, "error", err)
		return nil
	}

	var tf cjkTokenFile
	if err := json.Unmarshal(data, &tf); err != nil {
		logger.Error("failed to parse CJK block file", "path", path, "error", err)
		return nil
	}

	if len(tf.BlockedTokenIDs) == 0 {
		logger.Warn("CJK block file has no blocked tokens", "path", path)
		return nil
	}

	// Build logit_bias map and serialize once.
	bias := make(map[string]float64, len(tf.BlockedTokenIDs))
	for _, tid := range tf.BlockedTokenIDs {
		bias[fmt.Sprintf("%d", tid)] = -100
	}

	raw, err := json.Marshal(bias)
	if err != nil {
		logger.Error("failed to serialize CJK logit_bias", "error", err)
		return nil
	}

	logger.Info("CJK block loaded",
		"path", path,
		"model", tf.Model,
		"blocked", len(tf.BlockedTokenIDs),
		"korean_preserved", tf.KoreanCount,
		"mixed_kept", tf.MixedCount,
		"logit_bias_bytes", len(raw),
	)

	return &cjkBlock{
		raw:   json.RawMessage(raw),
		count: len(tf.BlockedTokenIDs),
	}
}

// mergeInto injects the pre-serialized logit_bias into an ExtraBody map.
// json.RawMessage implements json.Marshaler, so mergeJSONFields in the llm
// package passes it through without re-encoding — zero per-request cost.
func (b *cjkBlock) mergeInto(extra map[string]any) {
	if b == nil || len(b.raw) == 0 {
		return
	}
	extra["logit_bias"] = b.raw
}
