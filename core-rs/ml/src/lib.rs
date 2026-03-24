//! Local ML inference for Deneb (GGUF models via llama.cpp).
//!
//! Provides text embedding, reranking, and query expansion
//! using locally-hosted GGUF models.

pub mod embedder;
pub mod reranker;
pub mod manager;
