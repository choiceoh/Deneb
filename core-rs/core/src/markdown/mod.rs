//! Markdown IR processing — span manipulation, fence parsing, code span
//! detection, and marker-based rendering.
//!
//! Mirrors the TypeScript implementation in `src/markdown/` and exposes
//! both a Rust API and napi-rs bindings for Node.js.

pub mod code_spans;
pub mod fences;
pub mod parser;
pub mod render;
mod render_state;
pub mod spans;
mod spoilers;
mod tables;
