//! Text embedding via local GGUF models.
//!
//! Port of Python vega/ml/embedder.py.

/// Result of embedding a batch of texts.
#[derive(Debug)]
pub struct EmbeddingResult {
    /// Embeddings as row-major f32 vectors (num_texts x dim).
    pub vectors: Vec<Vec<f32>>,
    /// Embedding dimensionality.
    pub dim: usize,
}

// TODO(phase1): Implement LocalEmbedder using llama-cpp-2 crate.
// - Load Qwen3-Embedding-8B GGUF model
// - Batch text embedding with L2 normalization
// - Thread-safe model access
