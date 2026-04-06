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

    /// LoRA adapter file not found.
    #[error("LoRA adapter not found: {0}")]
    LoraAdapterNotFound(String),

    /// LoRA adapter loading failed.
    #[error("LoRA adapter load failed: {0}")]
    LoraAdapterLoadFailed(String),

    /// LoRA adapter context application failed.
    #[error("LoRA adapter set failed: {0}")]
    LoraAdapterSetFailed(String),
}
