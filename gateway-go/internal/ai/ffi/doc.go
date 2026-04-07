// Package ffi provides pure-Go implementations of functionality that was
// previously backed by the Rust deneb-core library via CGo.
//
// Security & Protocol:
//   - ValidateFrame: Gateway frame JSON validation
//   - ConstantTimeEq: Constant-time byte comparison
//   - ValidateSessionKey: Session key format validation
//   - SanitizeHTML: HTML entity escaping
//   - IsSafeURL: SSRF URL validation
//   - ValidateErrorCode: Error code string validation
//   - ValidateParams: RPC parameter schema validation
//
// Media:
//   - DetectMIME: Magic-byte MIME type detection
//
// Parsing:
//   - ExtractLinks: URL extraction with SSRF checks
//   - HTMLToMarkdown: HTML to Markdown conversion
//   - Base64Estimate / Base64Canonicalize: Base64 utilities
//   - ParseMediaTokens: MEDIA: token extraction
//
// Markdown:
//   - MarkdownToIR: Markdown to IR parser (goldmark-based)
//   - MarkdownDetectFences: Fenced code block detection
//   - MarkdownToPlainText: Markdown stripping convenience
//
// Compaction:
//   - CompactionEvaluate: pure-Go threshold evaluation
package ffi
