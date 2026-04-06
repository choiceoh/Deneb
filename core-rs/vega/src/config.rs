//! Central configuration for Vega.
//!
//! Port of Python vega/config.py: DB paths, schema version, model settings.

use std::path::PathBuf;

/// Schema version for the Vega `SQLite` database.
/// v7: added `source_file` column to chunks table.
pub const SCHEMA_VERSION: u32 = 7;

/// Vega protocol version for Deneb compatibility.
pub const PROTOCOL_VERSION: u32 = 1;

/// Vega version string.
pub const VERSION: &str = "2.0.0";

/// Runtime configuration for Vega.
#[derive(Debug, Clone)]
pub struct VegaConfig {
    /// Path to the `SQLite` database file.
    pub db_path: PathBuf,
    /// Directory containing project markdown files.
    pub md_dir: PathBuf,
    /// Reranking mode: "`vega_only`" (cosine + BM25 fusion) or "none" (BM25 only).
    pub rerank_mode: String,
    /// Inference backend: "sglang" (default, embeddings via Go HTTP) or "`sqlite_only`" (FTS only).
    pub inference_backend: String,
}

impl Default for VegaConfig {
    fn default() -> Self {
        Self {
            db_path: PathBuf::from("projects.db"),
            md_dir: PathBuf::from("projects"),
            rerank_mode: "vega_only".into(),
            inference_backend: "sglang".into(),
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
        if let Ok(v) = std::env::var("VEGA_INFERENCE") {
            cfg.inference_backend = v;
        }

        cfg
    }

    /// Check if the inference backend uses `SGLang` (embeddings provided externally via Go).
    pub fn has_sglang(&self) -> bool {
        self.inference_backend == "sglang"
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
                    .filter_map(std::result::Result::ok)
                    .any(|e| e.path().extension().is_some_and(|ext| ext == "md"))
            })
            .unwrap_or(false)
    }
}
