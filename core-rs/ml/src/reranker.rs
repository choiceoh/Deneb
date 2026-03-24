//! Document reranking via local GGUF models.
//!
//! Port of Python vega/ml/reranker.py.

/// A scored document from reranking.
#[derive(Debug, Clone)]
pub struct RankedDocument {
    /// Original index in the input list.
    pub index: usize,
    /// Relevance score from the reranker.
    pub score: f64,
}

// TODO(phase1): Implement LocalReranker using llama-cpp-2 crate.
// - Load Qwen3-Reranker-4B GGUF model
// - Query-document pair scoring
// - Batch reranking with configurable top-k
