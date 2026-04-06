//go:build no_ffi || !cgo

package ffi

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/coremarkdown"
)

// MarkdownToIR parses markdown text into an intermediate representation using
// the pure-Go goldmark-based parser. Returns JSON-encoded IR with text, styles,
// links, and code block detection.
func MarkdownToIR(markdown string, optionsJSON string) (json.RawMessage, error) {
	if len(markdown) == 0 {
		return json.RawMessage(`{"text":"","styles":[],"links":[],"has_code_blocks":false}`), nil
	}

	// Check cache (shared with cgo path pattern).
	cacheKey := fnv1a64noffi(markdown + "|" + optionsJSON)
	if cached, ok := noffiCache.get(cacheKey); ok {
		return cached, nil
	}

	opts := coremarkdown.DefaultParseOptions()
	if len(optionsJSON) > 0 {
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
	noffiCache.put(cacheKey, result)
	return result, nil
}

// MarkdownDetectFences detects fenced code blocks in markdown text.
// Returns JSON array of fence span objects.
func MarkdownDetectFences(text string) (json.RawMessage, error) {
	if len(text) == 0 {
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

// noffi-only LRU cache (same pattern as cgo cache in markdown_cgo.go).
var noffiCache = &noffiMarkdownCache{entries: make(map[uint64]*noffiCacheEntry)}

const noffiCacheMaxEntries = 128

type noffiCacheEntry struct {
	value      json.RawMessage
	lastAccess int64
}

type noffiMarkdownCache struct {
	mu        sync.Mutex
	entries   map[uint64]*noffiCacheEntry
	accessCtr int64
}

func fnv1a64noffi(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

func (c *noffiMarkdownCache) get(key uint64) (json.RawMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.accessCtr++
	e.lastAccess = c.accessCtr
	return e.value, true
}

func (c *noffiMarkdownCache) put(key uint64, val json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessCtr++
	if len(c.entries) >= noffiCacheMaxEntries {
		var lruKey uint64
		lruAccess := c.accessCtr + 1
		for k, e := range c.entries {
			if e.lastAccess < lruAccess {
				lruAccess = e.lastAccess
				lruKey = k
			}
		}
		delete(c.entries, lruKey)
	}
	c.entries[key] = &noffiCacheEntry{value: val, lastAccess: c.accessCtr}
}
