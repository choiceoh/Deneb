//! Error types for the ML inference crate.

/// Errors from ML inference operations.
#[derive(Debug, thiserror::Error)]
pub enum MlError {
    /// llama-cpp-2 feature not compiled in.
    #[error("ML backend unavailable (compile with --features llama)")]
    BackendUnavailable,

    /// Model file not found on disk.
    #[error("model file not found: {0}")]
    ModelNotFound(String),

    /// Model failed to load (corrupt GGUF, OOM, etc.).
    #[error("model load failed: {0}")]
    LoadFailed(String),

    /// Inference error during embed/rerank.
    #[error("inference error: {0}")]
    InferenceFailed(String),

    /// Invalid input (empty texts, etc.).
    #[error("invalid input: {0}")]
    InvalidInput(String),

    /// JSON serialization/deserialization error.
    #[error("JSON error: {0}")]
    Json(#[from] serde_json::Error),
}
