//go:build !no_ffi && cgo

package ffi

/*
// Markdown FFI functions (from core-rs/core/src/lib.rs).
extern int deneb_markdown_to_ir(
	const unsigned char *md_ptr, unsigned long md_len,
	const unsigned char *opts_ptr, unsigned long opts_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_markdown_detect_fences(
	const unsigned char *text_ptr, unsigned long text_len,
	unsigned char *out_ptr, unsigned long out_len);
*/
import "C"
import (
	"encoding/json"
	"fmt"
	"sync"
	"unsafe"
)

// mdIRCache caches MarkdownToIR results by input hash to avoid redundant
// FFI calls for identical markdown content. Uses a bounded LRU cache
// with single-entry eviction based on a monotonic access counter.
var mdIRCache = &markdownCache{entries: make(map[uint64]*mdCacheEntry)}

const mdCacheMaxEntries = 128

type mdCacheEntry struct {
	value      json.RawMessage
	lastAccess int64
}

type markdownCache struct {
	mu        sync.Mutex
	entries   map[uint64]*mdCacheEntry
	accessCtr int64 // monotonic counter, cheaper than wall clock
}

// fnv1a64 is a fast non-cryptographic hash for cache keys.
func fnv1a64(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

func (c *markdownCache) get(key uint64) (json.RawMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.accessCtr++
	entry.lastAccess = c.accessCtr
	return entry.value, true
}

func (c *markdownCache) put(key uint64, val json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessCtr++
	if len(c.entries) >= mdCacheMaxEntries {
		// LRU eviction: remove the single least recently accessed entry.
		var lruKey uint64
		var lruAccess int64 = c.accessCtr + 1
		for k, e := range c.entries {
			if e.lastAccess < lruAccess {
				lruAccess = e.lastAccess
				lruKey = k
			}
		}
		delete(c.entries, lruKey)
	}
	c.entries[key] = &mdCacheEntry{value: val, lastAccess: c.accessCtr}
}

// MarkdownToIR parses markdown text into an intermediate representation.
// Returns JSON-encoded IR with text, styles, links, and code block detection.
// optionsJSON may be empty for default parse options.
func MarkdownToIR(markdown string, optionsJSON string) (json.RawMessage, error) {
	if len(markdown) == 0 {
		return json.RawMessage(`{"text":"","styles":[],"links":[],"has_code_blocks":false}`), nil
	}

	// Check cache for identical markdown (common in streaming where chunks repeat).
	cacheKey := fnv1a64(markdown + "|" + optionsJSON)
	if cached, ok := mdIRCache.get(cacheKey); ok {
		return cached, nil
	}

	// Output is typically larger than input due to JSON structure.
	// Use 6x multiplier with 16 KB floor to handle complex markdown safely.
	outSize := initialBufSize(len(markdown), 6, 16384)
	out := make([]byte, outSize)

	mdPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(markdown)))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	var optsPtr *C.uchar
	var optsLen C.ulong
	if len(optionsJSON) > 0 {
		optsPtr = (*C.uchar)(unsafe.Pointer(unsafe.StringData(optionsJSON)))
		optsLen = C.ulong(len(optionsJSON))
	}

	rc := C.deneb_markdown_to_ir(
		mdPtr, C.ulong(len(markdown)),
		optsPtr, optsLen,
		outPtr, C.ulong(len(out)),
	)
	if rc < 0 {
		return nil, ffiError("markdown_to_ir", int(rc))
	}
	result := json.RawMessage(out[:rc])
	mdIRCache.put(cacheKey, result)
	return result, nil
}

// MarkdownDetectFences detects fenced code blocks in markdown text.
// Returns JSON array of fence span objects.
func MarkdownDetectFences(text string) (json.RawMessage, error) {
	if len(text) == 0 {
		return json.RawMessage("[]"), nil
	}

	outSize := initialBufSize(len(text), 2, 4096)
	out := make([]byte, outSize)

	textPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(text)))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_markdown_detect_fences(
		textPtr, C.ulong(len(text)),
		outPtr, C.ulong(len(out)),
	)
	if rc < 0 {
		return nil, ffiError("markdown_detect_fences", int(rc))
	}
	return json.RawMessage(out[:rc]), nil
}

// MarkdownToPlainText is a convenience wrapper that parses markdown and returns
// only the plain text content (stripping all formatting).
func MarkdownToPlainText(markdown string) (string, error) {
	raw, err := MarkdownToIR(markdown, "")
	if err != nil {
		return "", fmt.Errorf("ffi: markdown_to_plain_text: %w", err)
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("ffi: markdown_to_plain_text: invalid JSON: %w", err)
	}
	return result.Text, nil
}
