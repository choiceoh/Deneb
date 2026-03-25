//! Text embedding via local GGUF models.
//!
//! Port of Python `vega/ml/embedder.py`.
//!
//! Uses Qwen3-Embedding-8B to produce L2-normalized embedding vectors.

use crate::error::MlError;
use crate::manager::{ModelManager, ModelRole};

/// Result of embedding a batch of texts.
#[derive(Debug, serde::Serialize)]
pub struct EmbeddingResult {
    /// Embeddings as row-major f32 vectors (num_texts × dim).
    pub vectors: Vec<Vec<f32>>,
    /// Embedding dimensionality.
    pub dim: usize,
}

/// Text → vector embedder backed by a GGUF model.
pub struct LocalEmbedder {
    manager: ModelManager,
}

impl LocalEmbedder {
    pub fn new(manager: ModelManager) -> Self {
        Self { manager }
    }

    /// Embed a batch of texts into L2-normalized vectors.
    pub fn embed(&self, texts: &[&str]) -> Result<EmbeddingResult, MlError> {
        if texts.is_empty() {
            return Err(MlError::InvalidInput("empty text list".into()));
        }

        let _model = self.manager.get_model(ModelRole::Embedder)?;

        #[cfg(feature = "llama")]
        {
            self.embed_with_model(&_model.model, texts)
        }

        #[cfg(not(feature = "llama"))]
        {
            Err(MlError::BackendUnavailable)
        }
    }

    /// Embed a single text, returning a 1D vector.
    pub fn embed_single(&self, text: &str) -> Result<Vec<f32>, MlError> {
        if text.is_empty() {
            return Err(MlError::InvalidInput("empty text".into()));
        }
        let result = self.embed(&[text])?;
        result
            .vectors
            .into_iter()
            .next()
            .ok_or_else(|| MlError::InferenceFailed("no vector produced".into()))
    }

    /// Real embedding path using llama-cpp-2.
    #[cfg(feature = "llama")]
    fn embed_with_model(
        &self,
        model: &llama_cpp_2::LlamaModel,
        texts: &[&str],
    ) -> Result<EmbeddingResult, MlError> {
        use llama_cpp_2::context::params::LlamaContextParams;
        use llama_cpp_2::llama_batch::LlamaBatch;

        let ctx_params = LlamaContextParams::default()
            .with_n_ctx(std::num::NonZeroU32::new(512))
            .with_embeddings(true);
        let mut ctx = model
            .new_context(None, ctx_params)
            .map_err(|e| MlError::InferenceFailed(format!("context creation: {e}")))?;

        let mut all_vectors: Vec<Vec<f32>> = Vec::with_capacity(texts.len());
        let mut dim = 0usize;

        // Process texts one by one (simple; batch optimization possible later).
        for text in texts {
            let tokens = model
                .str_to_token(text, llama_cpp_2::model::AddBos::Always)
                .map_err(|e| MlError::InferenceFailed(format!("tokenization: {e}")))?;

            let mut batch = LlamaBatch::new(tokens.len(), 1);
            for (i, &token) in tokens.iter().enumerate() {
                let is_last = i == tokens.len() - 1;
                batch
                    .add(token, i as i32, &[0], is_last)
                    .map_err(|e| MlError::InferenceFailed(format!("batch add: {e}")))?;
            }

            ctx.decode(&mut batch)
                .map_err(|e| MlError::InferenceFailed(format!("decode: {e}")))?;

            // Get embedding from the last token position.
            let emb = ctx
                .embeddings_seq_ith(0)
                .map_err(|e| MlError::InferenceFailed(format!("embeddings: {e}")))?;

            let vec: Vec<f32> = emb.to_vec();
            if dim == 0 {
                dim = vec.len();
            }

            // L2 normalize.
            let normalized = l2_normalize(&vec);
            all_vectors.push(normalized);

            ctx.clear_kv_cache();
        }

        Ok(EmbeddingResult {
            vectors: all_vectors,
            dim,
        })
    }
}

/// L2-normalize a vector (cosine similarity → dot product).
#[cfg_attr(not(feature = "llama"), allow(dead_code))]
fn l2_normalize(v: &[f32]) -> Vec<f32> {
    let norm: f32 = v.iter().map(|x| x * x).sum::<f32>().sqrt();
    if norm == 0.0 {
        return v.to_vec();
    }
    v.iter().map(|x| x / norm).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn l2_normalize_unit_vector() {
        let v = vec![1.0, 0.0, 0.0];
        let n = l2_normalize(&v);
        assert!((n[0] - 1.0).abs() < 1e-6);
        assert!((n[1]).abs() < 1e-6);
    }

    #[test]
    fn l2_normalize_general() {
        let v = vec![3.0, 4.0];
        let n = l2_normalize(&v);
        let norm: f32 = n.iter().map(|x| x * x).sum::<f32>().sqrt();
        assert!((norm - 1.0).abs() < 1e-5);
    }

    #[test]
    fn l2_normalize_zero_vector() {
        let v = vec![0.0, 0.0, 0.0];
        let n = l2_normalize(&v);
        assert_eq!(n, v);
    }

    #[test]
    #[cfg(not(feature = "llama"))]
    fn embed_without_backend() {
        let mgr = ModelManager::new(vec![crate::manager::ModelConfig::embedder(
            std::path::PathBuf::from("/nonexistent.gguf"),
            300,
        )]);
        let embedder = LocalEmbedder::new(mgr);
        let err = embedder.embed(&["hello"]).unwrap_err();
        assert!(matches!(err, MlError::BackendUnavailable));
    }

    #[test]
    fn embed_empty_input() {
        let mgr = ModelManager::new(vec![]);
        let embedder = LocalEmbedder::new(mgr);
        let err = embedder.embed(&[]).unwrap_err();
        assert!(matches!(err, MlError::InvalidInput(_)));
    }

    #[test]
    fn embed_single_empty_text() {
        let mgr = ModelManager::new(vec![]);
        let embedder = LocalEmbedder::new(mgr);
        let err = embedder.embed_single("").unwrap_err();
        assert!(matches!(err, MlError::InvalidInput(_)));
    }
}
