//! Pre-LLM parsing operations exposed via C FFI for the Go gateway.
//!
//! Ports CPU-heavy TypeScript parsing (URL extraction, HTML→Markdown,
//! base64 validation, media-token extraction) to Rust so the Go gateway
//! can call them directly without a Node.js bridge hop.

pub mod base64_util;
pub mod html_to_markdown;
pub mod media_tokens;
pub mod url_extract;
