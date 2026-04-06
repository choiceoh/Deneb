package ffi

import (
	"errors"

	"github.com/choiceoh/deneb/gateway-go/internal/coreparsing/base64util"
	"github.com/choiceoh/deneb/gateway-go/internal/coreparsing/htmlmd"
	"github.com/choiceoh/deneb/gateway-go/internal/coreparsing/mediatokens"
	"github.com/choiceoh/deneb/gateway-go/internal/coreparsing/urlextract"
)

// ExtractLinks is a pure-Go fallback for URL extraction.
// Delegates to coreparsing/urlextract (ported from Rust core).
func ExtractLinks(text string, maxLinks int) ([]string, error) {
	return urlextract.ExtractLinks(text, maxLinks), nil
}

// HtmlToMarkdown is a pure-Go fallback for HTML to Markdown conversion.
// Delegates to the coreparsing/htmlmd tokenizer+emitter (ported from Rust core).
func HtmlToMarkdown(html string) (text string, title string, err error) {
	if len(html) == 0 {
		return "", "", nil
	}
	r := htmlmd.Convert(html)
	return r.Text, r.Title, nil
}

// Base64Estimate is a pure-Go fallback for base64 decoded size estimation.
// Delegates to coreparsing/base64util (ported from Rust core).
func Base64Estimate(input string) (int64, error) {
	return base64util.Estimate(input), nil
}

// Base64Canonicalize is a pure-Go fallback for base64 validation.
// Delegates to coreparsing/base64util (ported from Rust core).
func Base64Canonicalize(input string) (string, error) {
	result, err := base64util.Canonicalize(input)
	if err != nil {
		return "", errors.New("ffi: base64_canonicalize: invalid base64")
	}
	return result, nil
}

// HtmlToMarkdownStripNoise is a pure-Go fallback with noise element stripping.
// Suppresses nav, aside, svg, iframe, form in addition to script/style/noscript.
func HtmlToMarkdownStripNoise(html string) (text string, title string, err error) {
	if len(html) == 0 {
		return "", "", nil
	}
	r := htmlmd.ConvertWithOpts(html, htmlmd.Options{StripNoise: true})
	return r.Text, r.Title, nil
}

// ParseMediaTokens is a pure-Go fallback for MEDIA: token extraction.
// Delegates to coreparsing/mediatokens (ported from Rust core).
func ParseMediaTokens(text string) (cleanText string, mediaURLs []string, audioAsVoice bool, err error) {
	r := mediatokens.Parse(text)
	return r.Text, r.MediaURLs, r.AudioAsVoice, nil
}
