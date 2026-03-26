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
    /// Reranking mode: "vega_only" (cosine + BM25 fusion) or "none" (BM25 only).
    pub rerank_mode: String,
    /// Inference backend: "sglang" (default, embeddings via Go HTTP) or "sqlite_only" (FTS only).
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

    /// Check if the inference backend uses SGLang (embeddings provided externally via Go).
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
                    .filter_map(|e| e.ok())
                    .any(|e| e.path().extension().is_some_and(|ext| ext == "md"))
            })
            .unwrap_or(false)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    #[test]
    fn test_constants() {
        assert_eq!(SCHEMA_VERSION, 6);
        assert_eq!(PROTOCOL_VERSION, 1);
        assert_eq!(VERSION, "2.0.0");
    }

    #[test]
    fn test_default_config() {
        let cfg = VegaConfig::default();
        assert_eq!(cfg.db_path, PathBuf::from("projects.db"));
        assert_eq!(cfg.md_dir, PathBuf::from("projects"));
        assert_eq!(cfg.rerank_mode, "vega_only");
        assert_eq!(cfg.inference_backend, "sglang");
    }

    #[test]
    fn test_has_sglang_default() {
        let cfg = VegaConfig::default();
        assert!(cfg.has_sglang());
    }

    #[test]
    fn test_has_sglang_sqlite_only() {
        let cfg = VegaConfig {
            inference_backend: "sqlite_only".into(),
            ..Default::default()
        };
        assert!(!cfg.has_sglang());
    }

    #[test]
    fn test_db_exists_nonexistent() {
        let cfg = VegaConfig {
            db_path: PathBuf::from("/nonexistent/path/test.db"),
            ..Default::default()
        };
        assert!(!cfg.db_exists());
    }

    #[test]
    fn test_db_exists_real_file() {
        let dir = tempfile::tempdir().unwrap();
        let db_path = dir.path().join("test.db");
        fs::write(&db_path, b"fake-db").unwrap();

        let cfg = VegaConfig {
            db_path,
            ..Default::default()
        };
        assert!(cfg.db_exists());
    }

    #[test]
    fn test_md_dir_valid_nonexistent() {
        let cfg = VegaConfig {
            md_dir: PathBuf::from("/nonexistent/dir"),
            ..Default::default()
        };
        assert!(!cfg.md_dir_valid());
    }

    #[test]
    fn test_md_dir_valid_empty_dir() {
        let dir = tempfile::tempdir().unwrap();
        let cfg = VegaConfig {
            md_dir: dir.path().to_path_buf(),
            ..Default::default()
        };
        assert!(!cfg.md_dir_valid());
    }

    #[test]
    fn test_md_dir_valid_with_md_files() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(dir.path().join("project.md"), b"# Project").unwrap();

        let cfg = VegaConfig {
            md_dir: dir.path().to_path_buf(),
            ..Default::default()
        };
        assert!(cfg.md_dir_valid());
    }

    #[test]
    fn test_md_dir_valid_no_md_files() {
        let dir = tempfile::tempdir().unwrap();
        fs::write(dir.path().join("readme.txt"), b"text").unwrap();

        let cfg = VegaConfig {
            md_dir: dir.path().to_path_buf(),
            ..Default::default()
        };
        assert!(!cfg.md_dir_valid());
    }
}
