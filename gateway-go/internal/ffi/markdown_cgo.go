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
	"unsafe"
)

// MarkdownToIR parses markdown text into an intermediate representation.
// Returns JSON-encoded IR with text, styles, links, and code block detection.
// optionsJSON may be empty for default parse options.
func MarkdownToIR(markdown string, optionsJSON string) (json.RawMessage, error) {
	if len(markdown) == 0 {
		return json.RawMessage(`{"text":"","styles":[],"links":[],"has_code_blocks":false}`), nil
	}

	// Output is typically larger than input due to JSON structure.
	// Use 6x multiplier with 16 KB floor to handle complex markdown safely.
	outSize := len(markdown) * 6
	if outSize < 16384 {
		outSize = 16384
	}
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
	return json.RawMessage(out[:rc]), nil
}

// MarkdownDetectFences detects fenced code blocks in markdown text.
// Returns JSON array of fence span objects.
func MarkdownDetectFences(text string) (json.RawMessage, error) {
	if len(text) == 0 {
		return json.RawMessage("[]"), nil
	}

	outSize := len(text) * 2
	if outSize < 4096 {
		outSize = 4096
	}
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
