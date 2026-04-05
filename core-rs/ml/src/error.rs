//! Error types for the ML inference crate.

/// Errors that can occur during local ML inference.
#[derive(Debug, thiserror::Error)]
pub enum MlError {
    /// The ML backend (llama.cpp) was not compiled in.
    #[error("ML backend unavailable (build with --features llama)")]
    BackendUnavailable,

    /// The specified model file was not found on disk.
    #[error("model not found: {0}")]
    ModelNotFound(String),

    /// Model loading or inference failed.
    #[error("inference failed: {0}")]
    InferenceFailed(String),

    /// Invalid input (empty text, etc.).
    #[error("invalid input: {0}")]
    InvalidInput(String),
}
