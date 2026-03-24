//go:build !no_ffi && cgo

package ffi

/*
// Parsing FFI functions (from core-rs/core/src/lib.rs).
extern int deneb_extract_links(
	const unsigned char *text_ptr, unsigned long text_len,
	const unsigned char *config_ptr, unsigned long config_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_html_to_markdown(
	const unsigned char *html_ptr, unsigned long html_len,
	unsigned char *out_ptr, unsigned long out_len);
extern long long deneb_base64_estimate(
	const unsigned char *input_ptr, unsigned long input_len);
extern int deneb_base64_canonicalize(
	const unsigned char *input_ptr, unsigned long input_len,
	unsigned char *out_ptr, unsigned long out_len);
extern int deneb_parse_media_tokens(
	const unsigned char *text_ptr, unsigned long text_len,
	unsigned char *out_ptr, unsigned long out_len);
*/
import "C"
import (
	"encoding/json"
	"errors"
	"fmt"
	"unsafe"
)

// ExtractLinks extracts safe URLs from message text using the Rust parser.
// Strips markdown link syntax, deduplicates, and SSRF-checks each URL.
func ExtractLinks(text string, maxLinks int) ([]string, error) {
	if len(text) == 0 {
		return nil, nil
	}
	config := fmt.Sprintf(`{"max_links":%d}`, maxLinks)

	// Output buffer: URLs are shorter than input text.
	outSize := len(text)
	if outSize < 4096 {
		outSize = 4096
	}
	out := make([]byte, outSize)

	textPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(text)))
	configPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(config)))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_extract_links(
		textPtr, C.ulong(len(text)),
		configPtr, C.ulong(len(config)),
		outPtr, C.ulong(len(out)),
	)
	if rc < 0 {
		return nil, ffiError("extract_links", int(rc))
	}

	var urls []string
	if err := json.Unmarshal(out[:rc], &urls); err != nil {
		return nil, fmt.Errorf("ffi: extract_links: invalid JSON output: %w", err)
	}
	return urls, nil
}

// HtmlToMarkdown converts HTML to a Markdown-like text representation.
// Returns the converted text and an optional title.
func HtmlToMarkdown(html string) (text string, title string, err error) {
	if len(html) == 0 {
		return "", "", nil
	}

	// Markdown output is typically shorter than HTML; allocate 2x for safety.
	outSize := len(html) * 2
	if outSize < 4096 {
		outSize = 4096
	}
	out := make([]byte, outSize)

	htmlPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(html)))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_html_to_markdown(
		htmlPtr, C.ulong(len(html)),
		outPtr, C.ulong(len(out)),
	)
	if rc < 0 {
		return "", "", ffiError("html_to_markdown", int(rc))
	}

	var result struct {
		Text  string  `json:"text"`
		Title *string `json:"title,omitempty"`
	}
	if err := json.Unmarshal(out[:rc], &result); err != nil {
		return "", "", fmt.Errorf("ffi: html_to_markdown: invalid JSON output: %w", err)
	}
	if result.Title != nil {
		return result.Text, *result.Title, nil
	}
	return result.Text, "", nil
}

// Base64Estimate estimates the decoded byte size of a base64 string
// without decoding it. Whitespace is skipped; padding is detected.
func Base64Estimate(input string) (int64, error) {
	if len(input) == 0 {
		return 0, nil
	}
	ptr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(input)))
	rc := C.deneb_base64_estimate(ptr, C.ulong(len(input)))
	if rc < 0 {
		return 0, ffiError("base64_estimate", int(rc))
	}
	return int64(rc), nil
}

// Base64Canonicalize validates and canonicalizes a base64 string by
// stripping whitespace and checking format. Returns empty string and
// error if invalid.
func Base64Canonicalize(input string) (string, error) {
	if len(input) == 0 {
		return "", errors.New("ffi: base64_canonicalize: empty input")
	}

	// Output is at most the same length as input (whitespace stripped).
	out := make([]byte, len(input))
	ptr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(input)))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_base64_canonicalize(
		ptr, C.ulong(len(input)),
		outPtr, C.ulong(len(out)),
	)
	if rc == -3 {
		return "", errors.New("ffi: base64_canonicalize: invalid base64")
	}
	if rc < 0 {
		return "", ffiError("base64_canonicalize", int(rc))
	}
	return string(out[:rc]), nil
}

// ParseMediaTokens extracts MEDIA: tokens from text output.
// Returns cleaned text, extracted media URLs, and audio_as_voice flag.
func ParseMediaTokens(text string) (cleanText string, mediaURLs []string, audioAsVoice bool, err error) {
	if len(text) == 0 {
		return "", nil, false, nil
	}

	outSize := len(text) + 4096
	out := make([]byte, outSize)

	textPtr := (*C.uchar)(unsafe.Pointer(unsafe.StringData(text)))
	outPtr := (*C.uchar)(unsafe.Pointer(&out[0]))

	rc := C.deneb_parse_media_tokens(
		textPtr, C.ulong(len(text)),
		outPtr, C.ulong(len(out)),
	)
	if rc < 0 {
		return text, nil, false, ffiError("parse_media_tokens", int(rc))
	}

	var result struct {
		Text         string   `json:"text"`
		MediaURLs    []string `json:"media_urls,omitempty"`
		AudioAsVoice bool     `json:"audio_as_voice,omitempty"`
	}
	if err := json.Unmarshal(out[:rc], &result); err != nil {
		return text, nil, false, fmt.Errorf("ffi: parse_media_tokens: invalid JSON output: %w", err)
	}
	return result.Text, result.MediaURLs, result.AudioAsVoice, nil
}

// ffiError maps negative FFI return codes to Go errors.
func ffiError(fn string, rc int) error {
	switch rc {
	case -1:
		return fmt.Errorf("ffi: %s: null pointer", fn)
	case -2:
		return fmt.Errorf("ffi: %s: invalid UTF-8", fn)
	case -3:
		return fmt.Errorf("ffi: %s: validation failed", fn)
	case -4:
		return fmt.Errorf("ffi: %s: input too large", fn)
	case -5:
		return fmt.Errorf("ffi: %s: JSON error", fn)
	case -6:
		return fmt.Errorf("ffi: %s: output buffer too small", fn)
	case -99:
		return fmt.Errorf("ffi: %s: rust panic", fn)
	default:
		return fmt.Errorf("ffi: %s: unknown error (rc=%d)", fn, rc)
	}
}
