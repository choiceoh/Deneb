//! Central configuration for Vega.
//!
//! Port of Python vega/config.py: DB paths, schema version, model settings.

use std::path::PathBuf;

/// Schema version for the Vega SQLite database.
/// Matches Python SCHEMA_VERSION = 6.
pub const SCHEMA_VERSION: u32 = 6;

/// Vega protocol version for Deneb compatibility.
pub const PROTOCOL_VERSION: u32 = 1;

/// Vega version string.
pub const VERSION: &str = "2.0.0";

/// Runtime configuration for Vega.
#[derive(Debug, Clone)]
pub struct VegaConfig {
    /// Path to the SQLite database file.
    pub db_path: PathBuf,
    /// Directory containing project markdown files.
    pub md_dir: PathBuf,
    /// Reranking mode: "full" (fusion + reranker), "vega_only" (fusion), or "none" (BM25 only).
    pub rerank_mode: String,
    /// Model unload TTL in seconds (0 = never unload).
    pub model_unload_ttl: u64,
    /// Inference backend: "local" or "sqlite_only".
    pub inference_backend: String,
    /// Path to embedder GGUF model (for semantic search).
    pub model_embedder: Option<PathBuf>,
    /// Path to reranker GGUF model.
    pub model_reranker: Option<PathBuf>,
}

impl Default for VegaConfig {
    fn default() -> Self {
        Self {
            db_path: PathBuf::from("projects.db"),
            md_dir: PathBuf::from("projects"),
            rerank_mode: "full".into(),
            model_unload_ttl: 300,
            inference_backend: "local".into(),
            model_embedder: None,
            model_reranker: None,
        }
    }
}

impl VegaConfig {
    /// Create config from environment variables with fallback defaults.
    pub fn from_env() -> Self {
        let mut cfg = Self::default();

        if let Ok(v) = std::env::var("DB_PATH") {
            cfg.db_path = PathBuf::from(v);
        }
        if let Ok(v) = std::env::var("MD_DIR") {
            cfg.md_dir = PathBuf::from(v);
        }
        if let Ok(v) = std::env::var("VEGA_RERANK") {
            cfg.rerank_mode = v;
        }
        if let Ok(v) = std::env::var("VEGA_MODEL_TTL") {
            if let Ok(n) = v.parse() {
                cfg.model_unload_ttl = n;
            }
        }
        if let Ok(v) = std::env::var("VEGA_INFERENCE") {
            cfg.inference_backend = v;
        }
        if let Ok(v) = std::env::var("VEGA_MODEL_EMBEDDER") {
            cfg.model_embedder = Some(PathBuf::from(v));
        }
        if let Ok(v) = std::env::var("VEGA_MODEL_RERANKER") {
            cfg.model_reranker = Some(PathBuf::from(v));
        }

        cfg
    }

    /// Check if ML inference is configured and available.
    pub fn has_ml(&self) -> bool {
        self.inference_backend == "local"
            && (self.model_embedder.is_some() || self.model_reranker.is_some())
    }

    /// Check if the database path exists.
    pub fn db_exists(&self) -> bool {
        self.db_path.is_file()
    }

    /// Check if the markdown directory exists and contains .md files.
    pub fn md_dir_valid(&self) -> bool {
        if !self.md_dir.is_dir() {
            return false;
        }
        std::fs::read_dir(&self.md_dir)
            .map(|entries| {
                entries
                    .filter_map(|e| e.ok())
                    .any(|e| e.path().extension().is_some_and(|ext| ext == "md"))
            })
            .unwrap_or(false)
    }
}
