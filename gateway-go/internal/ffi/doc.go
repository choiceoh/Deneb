// Package ffi provides the interface for functionality that was previously
// backed by the Rust deneb-core library via CGo. All functions are now
// implemented in pure Go; the Rust core has been fully removed.
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
//   - HtmlToMarkdown: HTML to Markdown conversion
//   - Base64Estimate / Base64Canonicalize: Base64 utilities
//   - ParseMediaTokens: MEDIA: token extraction
//
// Markdown:
//   - MarkdownToIR: Markdown to IR parser (goldmark-based)
//   - MarkdownDetectFences: Fenced code block detection
//   - MarkdownToPlainText: Markdown stripping convenience
//
// Context Engine (stubs, being replaced by RLM):
//   - ContextAssemblyNew/Start/Step: returns unavailable
//   - ContextExpandNew/Start/Step: returns unavailable
//   - ContextEngineDrop: no-op
//
// Compaction:
//   - CompactionEvaluate: pure-Go threshold evaluation
//   - CompactionSweepNew/Start/Step/Drop: returns unavailable
//
// ML (stubs):
//   - MLEmbed/MLEmbedCtx: returns unavailable
//   - MLAvailable: returns false
package ffi
