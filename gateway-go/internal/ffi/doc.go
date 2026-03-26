// Package ffi provides Go bindings to the Rust deneb-core library via CGo.
//
// The Rust library (core-rs/) is compiled to a C-compatible static library
// (libdeneb_core.a) and linked here via CGo.
//
// Exported function groups:
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
// Parsing (pre-LLM):
//   - ExtractLinks: URL extraction with SSRF checks
//   - HtmlToMarkdown: HTML to Markdown conversion
//   - Base64Estimate / Base64Canonicalize: Base64 utilities
//   - ParseMediaTokens: MEDIA: token extraction
//
// Memory Search (SIMD-accelerated):
//   - MemoryCosineSimilarity: Vector cosine similarity
//   - MemoryBm25RankToScore: BM25 rank normalization
//   - MemoryBuildFtsQuery: Full-text search query builder
//   - MemoryMergeHybridResults: Hybrid search merge pipeline
//   - MemoryExtractKeywords: Multilingual keyword extraction
//
// Markdown:
//   - MarkdownToIR: Markdown to IR parser (pulldown-cmark)
//   - MarkdownDetectFences: Fenced code block detection
//   - MarkdownToPlainText: Markdown stripping convenience
//
// Context Engine:
//   - ContextAssemblyNew/Start/Step: Aurora context assembly
//   - ContextExpandNew/Start/Step: Memory retrieval expansion
//   - ContextEngineDrop: Handle cleanup
//
// Compaction:
//   - CompactionEvaluate: Compaction threshold evaluation
//   - CompactionSweepNew/Start/Step/Drop: Sweep state machine
//
// Vega FFI (requires "vega" feature in deneb-core):
//   - VegaExecute: Execute a Vega command
//   - VegaSearch: Execute a Vega search query
//
// ML FFI (requires "ml" feature in deneb-core):
//   - MLEmbed: Generate text embeddings
//   - MLRerank: Rerank documents against a query
//
// Build requirements:
//   - Rust toolchain with cargo
//   - Run `make rust` first to produce the static library
//   - CGO_ENABLED=1 (default) when building Go
//
// When the Rust library is not available (e.g. CI without Rust, pure-Go
// development), use the `no_ffi` build tag to compile with pure-Go
// fallbacks instead: `go build -tags no_ffi ./...`
package ffi
