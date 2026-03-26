//! Model lifecycle manager (load, unload, TTL-based eviction).
//!
//! Port of Python `vega/ml/manager.py`.
//!
//! The manager holds loaded llama.cpp model instances behind `Arc<Mutex<>>`
//! and tracks last-used timestamps for TTL-based automatic unloading.

use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use crate::error::MlError;

/// Supported model roles.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, serde::Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ModelRole {
    Embedder,
    Reranker,
    Expander,
}

impl ModelRole {
    /// All known roles (for iteration in status reports).
    pub const ALL: [ModelRole; 3] = [
        ModelRole::Embedder,
        ModelRole::Reranker,
        ModelRole::Expander,
    ];
}

impl std::fmt::Display for ModelRole {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Embedder => write!(f, "embedder"),
            Self::Reranker => write!(f, "reranker"),
            Self::Expander => write!(f, "expander"),
        }
    }
}

/// Configuration for a single model.
#[derive(Debug, Clone)]
pub struct ModelConfig {
    pub role: ModelRole,
    pub path: PathBuf,
    /// Context window size (tokens).
    pub n_ctx: u32,
    /// Batch size for processing.
    pub n_batch: u32,
    /// Whether to enable embedding mode (embedder only).
    pub embedding: bool,
    /// Unload after this many seconds of inactivity (0 = never).
    pub unload_ttl_secs: u64,
    /// Number of threads for inference. 0 = use all available cores.
    pub n_threads: u32,
}

/// Detect optimal thread count for inference.
/// Uses all available cores minus 2 (reserve for Go runtime and OS),
/// with a minimum of 4.
fn default_n_threads() -> u32 {
    let cpus = std::thread::available_parallelism()
        .map(|n| n.get() as u32)
        .unwrap_or(4);
    // Reserve 2 cores for Go gateway + OS, minimum 4 inference threads.
    cpus.saturating_sub(2).max(4)
}

impl ModelConfig {
    /// Default config for embedder (Qwen3-Embedding-8B).
    pub fn embedder(path: PathBuf, ttl: u64) -> Self {
        Self {
            role: ModelRole::Embedder,
            path,
            n_ctx: 512,
            n_batch: 512,
            embedding: true,
            unload_ttl_secs: ttl,
            n_threads: default_n_threads(),
        }
    }

    /// Default config for reranker (Qwen3-Reranker-4B).
    pub fn reranker(path: PathBuf, ttl: u64) -> Self {
        Self {
            role: ModelRole::Reranker,
            path,
            n_ctx: 512,
            n_batch: 512,
            embedding: false,
            unload_ttl_secs: ttl,
            n_threads: default_n_threads(),
        }
    }

    /// Default config for expander (Qwen3.5-9B).
    pub fn expander(path: PathBuf, ttl: u64) -> Self {
        Self {
            role: ModelRole::Expander,
            path,
            n_ctx: 2048,
            n_batch: 512,
            embedding: false,
            unload_ttl_secs: ttl,
            n_threads: default_n_threads(),
        }
    }
}

// ---------------------------------------------------------------------------
// Loaded model handle — wraps the llama-cpp-2 model (or a stub when the
// `llama` feature is disabled).
// ---------------------------------------------------------------------------

/// Opaque handle to a loaded GGUF model.
///
/// When the `llama` feature is enabled this wraps a real `llama_cpp_2::LlamaModel`.
/// Without it, construction always fails with [`MlError::BackendUnavailable`].
#[cfg(feature = "llama")]
#[derive(Debug)]
pub(crate) struct LoadedModel {
    pub model: llama_cpp_2::LlamaModel,
}

#[cfg(not(feature = "llama"))]
#[derive(Debug)]
pub(crate) struct LoadedModel {
    _private: (),
}

/// Entry for a loaded model in the manager.
struct ModelEntry {
    #[cfg_attr(not(feature = "llama"), allow(dead_code))]
    model: LoadedModel,
    last_used: Instant,
}

/// Model manager handles loading and lifecycle of GGUF models.
///
/// Thread-safe via internal `Mutex`. Clone shares the same underlying state.
#[derive(Clone)]
pub struct ModelManager {
    inner: Arc<Mutex<ManagerInner>>,
}

struct ManagerInner {
    configs: HashMap<ModelRole, ModelConfig>,
    models: HashMap<ModelRole, ModelEntry>,
}

impl ModelManager {
    /// Create a manager from a list of model configs.
    pub fn new(configs: Vec<ModelConfig>) -> Self {
        let map: HashMap<ModelRole, ModelConfig> =
            configs.into_iter().map(|c| (c.role, c)).collect();
        Self {
            inner: Arc::new(Mutex::new(ManagerInner {
                configs: map,
                models: HashMap::new(),
            })),
        }
    }

    /// Get (or lazy-load) a model for the given role.
    ///
    /// Returns an `Arc<LoadedModel>` on success. The model stays loaded in the
    /// manager until unloaded or TTL-expired.
    ///
    /// `LoadedModel` is intentionally `pub(crate)` — external callers use
    /// `LocalEmbedder`/`LocalReranker` which wrap this method.
    #[allow(private_interfaces)]
    pub fn get_model(&self, role: ModelRole) -> Result<Arc<LoadedModel>, MlError> {
        let mut inner = self.inner.lock().expect("model manager lock poisoned");

        // Already loaded — touch timestamp and return.
        if inner.models.contains_key(&role) {
            // Unwrap safe: contains_key confirmed above.
            let entry = inner.models.get_mut(&role).expect("role just checked");
            entry.last_used = Instant::now();
            // Safety: we return an Arc clone so the caller can use it after unlock.
            let model = Arc::new(LoadedModel {
                #[cfg(feature = "llama")]
                model: entry.model.model.clone(),
                #[cfg(not(feature = "llama"))]
                _private: (),
            });
            return Ok(model);
        }

        // Not loaded — try loading.
        let config = inner
            .configs
            .get(&role)
            .ok_or_else(|| MlError::ModelNotFound(format!("no config for role {role}")))?
            .clone();

        let loaded = Self::load_model(&config)?;
        let model_arc = Arc::new(LoadedModel {
            #[cfg(feature = "llama")]
            model: loaded.model.clone(),
            #[cfg(not(feature = "llama"))]
            _private: (),
        });

        inner.models.insert(
            role,
            ModelEntry {
                model: loaded,
                last_used: Instant::now(),
            },
        );

        Ok(model_arc)
    }

    /// Get the config for a given role (if configured).
    pub fn get_config(&self, role: ModelRole) -> Option<ModelConfig> {
        let inner = self.inner.lock().unwrap_or_else(|e| e.into_inner());
        inner.configs.get(&role).cloned()
    }

    /// Unload a specific model (or all if `role` is `None`).
    pub fn unload(&self, role: Option<ModelRole>) {
        let mut inner = self.inner.lock().expect("model manager lock poisoned");
        match role {
            Some(r) => {
                inner.models.remove(&r);
            }
            None => {
                inner.models.clear();
            }
        }
    }

    /// Unload models that have exceeded their TTL.
    pub fn unload_expired(&self) {
        let mut inner = self.inner.lock().expect("model manager lock poisoned");
        let now = Instant::now();
        let expired: Vec<ModelRole> = inner
            .models
            .iter()
            .filter_map(|(role, entry)| {
                let cfg = inner.configs.get(role)?;
                if cfg.unload_ttl_secs == 0 {
                    return None; // Never unload.
                }
                let ttl = Duration::from_secs(cfg.unload_ttl_secs);
                if now.duration_since(entry.last_used) > ttl {
                    Some(*role)
                } else {
                    None
                }
            })
            .collect();

        for role in expired {
            inner.models.remove(&role);
        }
    }

    /// Status report for all configured roles.
    pub fn status(&self) -> serde_json::Value {
        let inner = self.inner.lock().expect("model manager lock poisoned");
        let mut result = serde_json::Map::new();

        for role in ModelRole::ALL {
            let config = inner.configs.get(&role);
            let loaded = inner.models.contains_key(&role);
            let path = config.map(|c| c.path.display().to_string());

            let (file_exists, file_size_mb) = path
                .as_ref()
                .and_then(|p| std::fs::metadata(p).ok())
                .map(|m| (true, (m.len() as f64 / 1_048_576.0 * 10.0).round() / 10.0))
                .unwrap_or((false, 0.0));

            result.insert(
                role.to_string(),
                serde_json::json!({
                    "path": path,
                    "file_exists": file_exists,
                    "file_size_mb": file_size_mb,
                    "loaded": loaded,
                }),
            );
        }

        result.insert(
            "llama_backend".into(),
            serde_json::Value::Bool(cfg!(feature = "llama")),
        );

        serde_json::Value::Object(result)
    }

    // -----------------------------------------------------------------------
    // Internal: model loading
    // -----------------------------------------------------------------------

    #[cfg(feature = "llama")]
    fn load_model(config: &ModelConfig) -> Result<LoadedModel, MlError> {
        use llama_cpp_2::llama_backend::LlamaBackend;
        use llama_cpp_2::model::params::LlamaModelParams;
        use llama_cpp_2::model::LlamaModel;

        if !config.path.is_file() {
            return Err(MlError::ModelNotFound(config.path.display().to_string()));
        }

        // Initialize backend (idempotent via once_cell internally).
        let backend = LlamaBackend::init().map_err(|e| MlError::LoadFailed(e.to_string()))?;

        let params = LlamaModelParams::default();
        let model = LlamaModel::load_from_file(&backend, &config.path, &params)
            .map_err(|e| MlError::LoadFailed(format!("{}: {e}", config.path.display())))?;

        Ok(LoadedModel { model })
    }

    #[cfg(not(feature = "llama"))]
    fn load_model(_config: &ModelConfig) -> Result<LoadedModel, MlError> {
        Err(MlError::BackendUnavailable)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn model_role_display() {
        assert_eq!(ModelRole::Embedder.to_string(), "embedder");
        assert_eq!(ModelRole::Reranker.to_string(), "reranker");
        assert_eq!(ModelRole::Expander.to_string(), "expander");
    }

    #[test]
    fn manager_new_empty() {
        let mgr = ModelManager::new(vec![]);
        let status = mgr.status();
        assert_eq!(status["llama_backend"], cfg!(feature = "llama"));
    }

    #[test]
    fn manager_status_with_configs() {
        let mgr = ModelManager::new(vec![ModelConfig::embedder(
            PathBuf::from("/nonexistent/model.gguf"),
            300,
        )]);
        let status = mgr.status();
        assert_eq!(status["embedder"]["loaded"], false);
        assert_eq!(status["embedder"]["file_exists"], false);
    }

    #[test]
    fn manager_unload_all_no_panic() {
        let mgr = ModelManager::new(vec![]);
        mgr.unload(None);
        mgr.unload(Some(ModelRole::Embedder));
    }

    #[test]
    fn unload_expired_no_panic() {
        let mgr = ModelManager::new(vec![]);
        mgr.unload_expired();
    }

    #[test]
    #[cfg(not(feature = "llama"))]
    fn get_model_without_backend_returns_unavailable() {
        let mgr = ModelManager::new(vec![ModelConfig::embedder(
            PathBuf::from("/nonexistent/model.gguf"),
            300,
        )]);
        let err = mgr.get_model(ModelRole::Embedder).unwrap_err();
        assert!(matches!(err, MlError::BackendUnavailable));
    }

    #[test]
    fn get_model_unconfigured_role() {
        let mgr = ModelManager::new(vec![]);
        let err = mgr.get_model(ModelRole::Expander).unwrap_err();
        assert!(matches!(err, MlError::ModelNotFound(_)));
    }

    #[test]
    fn model_config_helpers() {
        let e = ModelConfig::embedder(PathBuf::from("e.gguf"), 300);
        assert!(e.embedding);
        assert_eq!(e.n_ctx, 512);

        let r = ModelConfig::reranker(PathBuf::from("r.gguf"), 300);
        assert!(!r.embedding);

        let x = ModelConfig::expander(PathBuf::from("x.gguf"), 0);
        assert_eq!(x.n_ctx, 2048);
        assert_eq!(x.unload_ttl_secs, 0);
    }
}
