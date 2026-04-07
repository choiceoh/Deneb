package ffi

import (
	"encoding/json"
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/core/corecache"
	"github.com/choiceoh/deneb/gateway-go/internal/core/coremarkdown"
)

// MarkdownToIR parses markdown text into an intermediate representation using
// the pure-Go goldmark-based parser. Returns JSON-encoded IR with text, styles,
// links, and code block detection.
func MarkdownToIR(markdown, optionsJSON string) (json.RawMessage, error) {
	if markdown == "" {
		return json.RawMessage(`{"text":"","styles":[],"links":[],"has_code_blocks":false}`), nil
	}

	cacheKey := mdFnv1a64(markdown + "|" + optionsJSON)
	if cached, ok := mdCache.Get(cacheKey); ok {
		return cached, nil
	}

	opts := coremarkdown.DefaultParseOptions()
	if optionsJSON != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
			return nil, fmt.Errorf("markdown_to_ir: invalid options: %w", err)
		}
	}

	ir, hasTables := coremarkdown.MarkdownToIRWithMeta(markdown, &opts)
	hasCodeBlocks := false
	for _, s := range ir.Styles {
		if s.Style == coremarkdown.StyleCodeBlock {
			hasCodeBlocks = true
			break
		}
	}

	out := &coremarkdown.IROutput{
		Text:          ir.Text,
		Styles:        ir.Styles,
		Links:         ir.Links,
		HasCodeBlocks: hasCodeBlocks,
		HasTables:     hasTables,
	}
	result, err := coremarkdown.MarshalIROutput(out)
	if err != nil {
		return nil, fmt.Errorf("markdown_to_ir: marshal: %w", err)
	}
	mdCache.Put(cacheKey, result)
	return result, nil
}

// MarkdownDetectFences detects fenced code blocks in markdown text.
// Returns JSON array of fence span objects.
func MarkdownDetectFences(text string) (json.RawMessage, error) {
	if text == "" {
		return json.RawMessage("[]"), nil
	}
	spans := coremarkdown.DetectFences(text)
	if spans == nil {
		return json.RawMessage("[]"), nil
	}
	data, err := json.Marshal(spans)
	if err != nil {
		return nil, fmt.Errorf("markdown_detect_fences: marshal: %w", err)
	}
	return json.RawMessage(data), nil
}

// MarkdownToPlainText is a convenience wrapper that parses markdown and returns
// only the plain text content (stripping all formatting).
func MarkdownToPlainText(markdown string) (string, error) {
	return coremarkdown.ToPlainText(markdown), nil
}

// LRU cache for MarkdownToIR results (avoids redundant parsing during streaming).
var mdCache = corecache.NewLRU[uint64, json.RawMessage](128, 0)

func mdFnv1a64(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}
