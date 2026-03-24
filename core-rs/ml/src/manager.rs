//! Model lifecycle manager (load, unload, TTL-based eviction).
//!
//! Port of Python vega/ml/manager.py.

use std::path::PathBuf;

/// Supported model roles.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum ModelRole {
    Embedder,
    Reranker,
    Expander,
}

/// Configuration for a single model.
#[derive(Debug, Clone)]
pub struct ModelConfig {
    pub role: ModelRole,
    pub path: PathBuf,
    /// Unload after this many seconds of inactivity (0 = never).
    pub unload_ttl_secs: u64,
}

/// Model manager handles loading and lifecycle of GGUF models.
///
/// Phase 0: scaffolding only. Phase 1 will add llama-cpp-2 integration.
pub struct ModelManager {
    _configs: Vec<ModelConfig>,
}

impl ModelManager {
    pub fn new(configs: Vec<ModelConfig>) -> Self {
        Self { _configs: configs }
    }
}
