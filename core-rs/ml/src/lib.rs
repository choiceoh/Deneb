//! Local ML inference for Deneb.
//!
//! Provides GGUF embedding model inference and LoRA-enabled text generation
//! via llama.cpp. Feature-gated: `llama` enables CPU inference, `cuda` adds
//! GPU acceleration.
//!
//! Training (RL pipeline, IS loss, gradient updates) is handled externally
//! by sglang + Tinker-Atropos. This crate is inference-only.

pub mod embedder;
pub mod error;
pub mod lora;

pub use embedder::{embed_texts, EmbedResult};
pub use error::MlError;
pub use lora::{generate, load_lora_adapter, unload_lora_adapter, GenerateResult, LoraAdapterInfo};
