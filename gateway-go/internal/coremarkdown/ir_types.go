// Package coremarkdown provides a pure-Go markdown-to-IR parser using goldmark.
// It produces the same JSON structure as the Rust FFI (core-rs/core/src/markdown/).
package coremarkdown

import "encoding/json"

// MarkdownStyle enumerates the style kinds tracked in the IR.
// JSON values match Rust's serde snake_case serialization.
type MarkdownStyle string

const (
	StyleBold          MarkdownStyle = "bold"
	StyleItalic        MarkdownStyle = "italic"
	StyleStrikethrough MarkdownStyle = "strikethrough"
	StyleCode          MarkdownStyle = "code"
	StyleCodeBlock     MarkdownStyle = "code_block"
	StyleSpoiler       MarkdownStyle = "spoiler"
	StyleBlockquote    MarkdownStyle = "blockquote"
)

// StyleSpan is a range [Start, End) with an associated style.
type StyleSpan struct {
	Start int           `json:"start"`
	End   int           `json:"end"`
	Style MarkdownStyle `json:"style"`
}

// LinkSpan is a range [Start, End) linked to an href.
type LinkSpan struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Href  string `json:"href"`
}

// MarkdownIR is the core intermediate representation: plain text plus spans.
type MarkdownIR struct {
	Text   string      `json:"text"`
	Styles []StyleSpan `json:"styles"`
	Links  []LinkSpan  `json:"links"`
}

// IROutput is the full JSON shape returned by MarkdownToIR, matching the Rust
// FFI output: MarkdownIR fields plus derived booleans.
type IROutput struct {
	Text          string      `json:"text"`
	Styles        []StyleSpan `json:"styles"`
	Links         []LinkSpan  `json:"links"`
	HasCodeBlocks bool        `json:"has_code_blocks"`
	HasTables     bool        `json:"has_tables"`
}

// MarshalIROutput serializes an IROutput to JSON, ensuring empty slices
// are rendered as [] rather than null.
func MarshalIROutput(out *IROutput) (json.RawMessage, error) {
	if out.Styles == nil {
		out.Styles = []StyleSpan{}
	}
	if out.Links == nil {
		out.Links = []LinkSpan{}
	}
	return json.Marshal(out)
}

// FenceSpan describes a fenced code block's byte range in the source text.
type FenceSpan struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	OpenLine string `json:"openLine"`
	Marker   string `json:"marker"`
	Indent   string `json:"indent"`
}

// ParseOptions controls markdown parsing behavior.
// Field names use camelCase to match the Rust serde(rename_all = "camelCase").
type ParseOptions struct {
	Linkify         bool   `json:"linkify"`
	EnableSpoilers  bool   `json:"enableSpoilers"`
	HeadingStyle    string `json:"headingStyle"`    // "none" or "bold"
	BlockquotePrefix string `json:"blockquotePrefix"`
	Autolink        bool   `json:"autolink"`
	TableMode       string `json:"tableMode"` // "off", "bullets", or "code"
}

// DefaultParseOptions returns the same defaults as Rust's ParseOptions::default().
func DefaultParseOptions() ParseOptions {
	return ParseOptions{
		Linkify:      true,
		HeadingStyle: "none",
		Autolink:     true,
		TableMode:    "off",
	}
}
