//! Local ML inference for Deneb.
//!
//! Provides GGUF embedding model inference via llama.cpp.
//! Feature-gated: `llama` enables CPU inference, `cuda` adds GPU acceleration.

pub mod embedder;
pub mod error;

pub use embedder::{embed_texts, EmbedResult};
pub use error::MlError;
