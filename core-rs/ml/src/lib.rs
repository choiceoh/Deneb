//! Local ML inference for Deneb (GGUF models via llama.cpp).
//!
//! Provides text embedding, reranking, and query expansion
//! using locally-hosted GGUF models on DGX Spark.
//!
//! # Feature flags
//!
//! - `llama`: Enable llama-cpp-2 bindings for real inference.
//! - `cuda`: Enable CUDA acceleration (implies `llama`).
//!
//! Without `llama`, all inference calls return [`MlError::BackendUnavailable`].

pub mod embedder;
pub mod error;
pub mod manager;
pub mod reranker;

pub use embedder::{EmbeddingResult, LocalEmbedder};
pub use error::MlError;
pub use manager::{ModelConfig, ModelManager, ModelRole};
pub use reranker::{LocalReranker, RankedDocument};
