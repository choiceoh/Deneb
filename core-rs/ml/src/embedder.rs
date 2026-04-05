//! Local GGUF embedding inference via llama.cpp.
//!
//! Loads a GGUF embedding model (e.g. BGE-M3, Qwen3-Embedding) and produces
//! L2-normalized float32 vectors suitable for cosine similarity search.

use crate::error::MlError;

/// Result of an embedding operation.
#[derive(Debug, serde::Serialize)]
pub struct EmbedResult {
    /// L2-normalized embedding vectors, one per input text.
    pub vectors: Vec<Vec<f32>>,
    /// Dimensionality of each vector.
    pub dim: usize,
    /// Short model identifier derived from the filename.
    pub model: String,
}

// ---------------------------------------------------------------------------
// Feature-gated implementation
// ---------------------------------------------------------------------------

#[cfg(feature = "llama")]
mod inner {
    use super::*;
    use std::path::{Path, PathBuf};
    use std::sync::OnceLock;

    use llama_cpp_2::context::params::LlamaContextParams;
    use llama_cpp_2::llama_backend::LlamaBackend;
    use llama_cpp_2::llama_batch::LlamaBatch;
    use llama_cpp_2::model::params::LlamaModelParams;
    use llama_cpp_2::model::{AddBos, LlamaModel};

    // -----------------------------------------------------------------------
    // stderr suppression — llama.cpp writes verbose init/scheduling messages
    // directly via fprintf(stderr, ...) bypassing llama_log_set/ggml_log_set
    // callbacks. Redirect fd 2 to /dev/null for the duration of inference.
    // -----------------------------------------------------------------------

    /// RAII guard that redirects stderr to `/dev/null` and restores on drop.
    #[allow(unsafe_code)]
    struct SuppressStderr {
        saved_fd: libc::c_int,
    }

    #[allow(unsafe_code)]
    impl SuppressStderr {
        fn new() -> Option<Self> {
            // SAFETY: dup/dup2/open/close are standard POSIX fd operations.
            unsafe {
                let saved = libc::dup(libc::STDERR_FILENO);
                if saved < 0 {
                    return None;
                }
                let devnull =
                    libc::open(b"/dev/null\0".as_ptr().cast(), libc::O_WRONLY);
                if devnull < 0 {
                    libc::close(saved);
                    return None;
                }
                libc::dup2(devnull, libc::STDERR_FILENO);
                libc::close(devnull);
                Some(Self { saved_fd: saved })
            }
        }
    }

    #[allow(unsafe_code)]
    impl Drop for SuppressStderr {
        fn drop(&mut self) {
            unsafe {
                libc::dup2(self.saved_fd, libc::STDERR_FILENO);
                libc::close(self.saved_fd);
            }
        }
    }

    /// Global llama.cpp backend (idempotent init).
    static BACKEND: OnceLock<LlamaBackend> = OnceLock::new();

    fn backend() -> &'static LlamaBackend {
        BACKEND.get_or_init(|| {
            // Suppress callback-based logs before any llama.cpp call.
            llama_cpp_2::send_logs_to_tracing(
                llama_cpp_2::LogOptions::default().with_logs_enabled(false),
            );
            LlamaBackend::init().unwrap_or_else(|e| {
                panic!("llama backend init failed: {e}");
            })
        })
    }

    /// Singleton model holder — loaded once, kept forever (single-user DGX Spark).
    struct LoadedModel {
        model: LlamaModel,
        model_name: String,
    }

    static LOADED_MODEL: OnceLock<LoadedModel> = OnceLock::new();

    fn model_name_from_path(path: &Path) -> String {
        path.file_stem()
            .and_then(|s| s.to_str())
            .unwrap_or("unknown")
            .to_string()
    }

    fn get_or_load_model(model_path: &str) -> Result<&'static LoadedModel, MlError> {
        // If already loaded, verify same path (defensive).
        if let Some(loaded) = LOADED_MODEL.get() {
            return Ok(loaded);
        }

        let path = PathBuf::from(model_path);
        if !path.exists() {
            return Err(MlError::ModelNotFound(model_path.to_string()));
        }

        let _backend = backend();
        let params = LlamaModelParams::default();
        let model = LlamaModel::load_from_file(_backend, &path, &params)
            .map_err(|e| MlError::InferenceFailed(format!("model load: {e}")))?;

        let name = model_name_from_path(&path);

        // OnceLock::get_or_init is race-safe; if another thread loads first, ours is dropped.
        Ok(LOADED_MODEL.get_or_init(|| LoadedModel {
            model,
            model_name: name,
        }))
    }

    /// Embed one or more texts using the loaded GGUF model.
    pub fn embed_texts(model_path: &str, texts: &[&str]) -> Result<EmbedResult, MlError> {
        if texts.is_empty() {
            return Ok(EmbedResult {
                vectors: vec![],
                dim: 0,
                model: String::new(),
            });
        }

        // Suppress stderr for the entire inference flow (model load, context
        // creation, decode). Restored automatically when _suppress drops.
        let _suppress = SuppressStderr::new();

        let loaded = get_or_load_model(model_path)?;
        let mut vectors = Vec::with_capacity(texts.len());
        let mut dim = 0;

        // Create a fresh context per call (lightweight; model is the heavyweight).
        let ctx_params = LlamaContextParams::default()
            .with_n_ctx(std::num::NonZeroU32::new(8192))
            .with_embeddings(true);

        let mut ctx = loaded
            .model
            .new_context(backend(), ctx_params)
            .map_err(|e| MlError::InferenceFailed(format!("context create: {e}")))?;

        for text in texts {
            if text.is_empty() {
                // Return zero vector for empty input (dimension from first real embed).
                if dim > 0 {
                    vectors.push(vec![0.0; dim]);
                } else {
                    vectors.push(vec![]);
                }
                continue;
            }

            let tokens = loaded
                .model
                .str_to_token(text, AddBos::Always)
                .map_err(|e| MlError::InferenceFailed(format!("tokenize: {e}")))?;

            // Truncate to context size if needed.
            let max_tokens = 8192;
            let token_count = tokens.len().min(max_tokens);

            let mut batch = LlamaBatch::new(token_count, 1);
            for (i, &token) in tokens[..token_count].iter().enumerate() {
                let is_last = i == token_count - 1;
                batch
                    .add(token, i as i32, &[0], is_last)
                    .map_err(|e| MlError::InferenceFailed(format!("batch add: {e}")))?;
            }

            ctx.decode(&mut batch)
                .map_err(|e| MlError::InferenceFailed(format!("decode: {e}")))?;

            let emb = ctx
                .embeddings_seq_ith(0)
                .map_err(|e| MlError::InferenceFailed(format!("get embeddings: {e}")))?;

            let normalized = l2_normalize(emb);
            dim = normalized.len();
            vectors.push(normalized);

            ctx.clear_kv_cache();
        }

        // Back-fill zero vectors for any empty texts that were processed before
        // we knew the dimension.
        if dim > 0 {
            for v in &mut vectors {
                if v.is_empty() {
                    *v = vec![0.0; dim];
                }
            }
        }

        Ok(EmbedResult {
            vectors,
            dim,
            model: loaded.model_name.clone(),
        })
    }
}

#[cfg(not(feature = "llama"))]
mod inner {
    use super::*;

    pub fn embed_texts(_model_path: &str, _texts: &[&str]) -> Result<EmbedResult, MlError> {
        Err(MlError::BackendUnavailable)
    }
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/// Embed one or more texts using a local GGUF model.
///
/// The model is lazily loaded on first call and kept resident (single-user).
/// Returns L2-normalized float32 vectors.
pub fn embed_texts(model_path: &str, texts: &[&str]) -> Result<EmbedResult, MlError> {
    inner::embed_texts(model_path, texts)
}

/// L2-normalize a vector to unit length.
#[cfg(any(feature = "llama", test))]
fn l2_normalize(v: &[f32]) -> Vec<f32> {
    let norm: f64 = v
        .iter()
        .map(|&x| f64::from(x) * f64::from(x))
        .sum::<f64>()
        .sqrt();
    if norm == 0.0 {
        return v.to_vec();
    }
    let norm_f32 = norm as f32;
    v.iter().map(|&x| x / norm_f32).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_l2_normalize() {
        let v = vec![3.0, 4.0];
        let n = l2_normalize(&v);
        assert!((n[0] - 0.6).abs() < 1e-5);
        assert!((n[1] - 0.8).abs() < 1e-5);

        // Verify unit length.
        let len: f32 = n.iter().map(|x| x * x).sum::<f32>().sqrt();
        assert!((len - 1.0).abs() < 1e-5);
    }

    #[test]
    fn test_l2_normalize_zero() {
        let v = vec![0.0, 0.0, 0.0];
        let n = l2_normalize(&v);
        assert_eq!(n, v);
    }

    #[test]
    fn test_embed_empty_texts() {
        // Empty input should return empty result (regardless of backend).
        let result = embed_texts("/nonexistent/model.gguf", &[]);
        match result {
            Ok(r) => {
                assert!(r.vectors.is_empty());
                assert_eq!(r.dim, 0);
            }
            Err(MlError::BackendUnavailable) => {
                // Expected when compiled without llama feature.
            }
            Err(e) => panic!("unexpected error: {e}"),
        }
    }
}
