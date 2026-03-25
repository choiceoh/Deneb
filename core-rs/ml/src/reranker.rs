//! Document reranking via local GGUF models.
//!
//! Port of Python `vega/ml/reranker.py`.
//!
//! Uses Qwen3-Reranker-4B with a yes/no judgment prompt. The "yes" token
//! logprob is passed through sigmoid to produce a 0–1 relevance score.

use crate::error::MlError;
use crate::manager::{ModelManager, ModelRole};

/// A scored document from reranking.
#[derive(Debug, Clone, serde::Serialize)]
pub struct RankedDocument {
    /// Original index in the input list.
    pub index: usize,
    /// Relevance score (0.0–1.0, higher = more relevant).
    pub score: f64,
}

/// Qwen3-Reranker prompt template.
/// Instructs the model to judge relevance with a "yes"/"no" answer.
#[cfg_attr(not(feature = "llama"), allow(dead_code))]
const RERANKER_PROMPT_TEMPLATE: &str = "\
<|im_start|>system\nJudge whether the document is relevant to the search query. \
Answer only \"yes\" or \"no\".<|im_end|>\n\
<|im_start|>user\n<query>{query}</query>\n<document>{document}</document><|im_end|>\n\
<|im_start|>assistant\n";

/// Max chars for query/document in the reranker prompt.
#[cfg_attr(not(feature = "llama"), allow(dead_code))]
const MAX_QUERY_CHARS: usize = 1000;
#[cfg_attr(not(feature = "llama"), allow(dead_code))]
const MAX_DOC_CHARS: usize = 1500;

/// Cross-encoder reranker backed by a GGUF model.
pub struct LocalReranker {
    manager: ModelManager,
}

impl LocalReranker {
    pub fn new(manager: ModelManager) -> Self {
        Self { manager }
    }

    /// Score each document against the query, returning relevance scores (0.0–1.0).
    ///
    /// Returns one `RankedDocument` per input document, in the original order.
    pub fn rerank(&self, query: &str, documents: &[&str]) -> Result<Vec<RankedDocument>, MlError> {
        if documents.is_empty() {
            return Ok(vec![]);
        }
        if query.is_empty() {
            return Ok(documents
                .iter()
                .enumerate()
                .map(|(i, _)| RankedDocument {
                    index: i,
                    score: 0.0,
                })
                .collect());
        }

        let _model = self.manager.get_model(ModelRole::Reranker)?;

        #[cfg(feature = "llama")]
        {
            self.rerank_with_model(&_model.model, query, documents)
        }

        #[cfg(not(feature = "llama"))]
        {
            Err(MlError::BackendUnavailable)
        }
    }

    /// Return top-k results sorted by score descending.
    pub fn rerank_top_k(
        &self,
        query: &str,
        documents: &[&str],
        k: usize,
    ) -> Result<Vec<RankedDocument>, MlError> {
        let mut ranked = self.rerank(query, documents)?;
        ranked.sort_by(|a, b| {
            b.score
                .partial_cmp(&a.score)
                .unwrap_or(std::cmp::Ordering::Equal)
        });
        ranked.truncate(k);
        Ok(ranked)
    }

    /// Real reranking via llama-cpp-2.
    #[cfg(feature = "llama")]
    fn rerank_with_model(
        &self,
        model: &llama_cpp_2::LlamaModel,
        query: &str,
        documents: &[&str],
    ) -> Result<Vec<RankedDocument>, MlError> {
        use llama_cpp_2::context::params::LlamaContextParams;
        use llama_cpp_2::llama_batch::LlamaBatch;

        let ctx_params = LlamaContextParams::default().with_n_ctx(std::num::NonZeroU32::new(512));
        let mut ctx = model
            .new_context(None, ctx_params)
            .map_err(|e| MlError::InferenceFailed(format!("context creation: {e}")))?;

        let mut results = Vec::with_capacity(documents.len());

        for (idx, doc) in documents.iter().enumerate() {
            let score = match self.score_pair(&mut ctx, model, query, doc) {
                Ok(s) => s,
                Err(e) => {
                    // Log-and-continue: a single doc failure shouldn't kill the batch.
                    eprintln!("reranker: doc {idx} failed: {e}");
                    0.0
                }
            };
            results.push(RankedDocument { index: idx, score });
        }

        Ok(results)
    }

    /// Score a single query-document pair.
    #[cfg(feature = "llama")]
    fn score_pair(
        &self,
        ctx: &mut llama_cpp_2::context::LlamaContext,
        model: &llama_cpp_2::LlamaModel,
        query: &str,
        document: &str,
    ) -> Result<f64, MlError> {
        use llama_cpp_2::llama_batch::LlamaBatch;

        // Truncate inputs to avoid context overflow.
        let q = truncate_str(query, MAX_QUERY_CHARS);
        let d = truncate_str(document, MAX_DOC_CHARS);

        let prompt = RERANKER_PROMPT_TEMPLATE
            .replace("{query}", q)
            .replace("{document}", d);

        let tokens = model
            .str_to_token(&prompt, llama_cpp_2::model::AddBos::Always)
            .map_err(|e| MlError::InferenceFailed(format!("tokenization: {e}")))?;

        let mut batch = LlamaBatch::new(tokens.len() + 1, 1);
        for (i, &token) in tokens.iter().enumerate() {
            batch
                .add(token, i as i32, &[0], i == tokens.len() - 1)
                .map_err(|e| MlError::InferenceFailed(format!("batch add: {e}")))?;
        }

        ctx.decode(&mut batch)
            .map_err(|e| MlError::InferenceFailed(format!("decode: {e}")))?;

        // Get logits for the last token position and find "yes" token logprob.
        let logits = ctx.candidates_ith(tokens.len() as i32 - 1);

        // Find the "yes" and "no" token IDs.
        let yes_id = model
            .str_to_token("yes", llama_cpp_2::model::AddBos::Never)
            .ok()
            .and_then(|t| t.first().copied());
        let no_id = model
            .str_to_token("no", llama_cpp_2::model::AddBos::Never)
            .ok()
            .and_then(|t| t.first().copied());

        let score = match (yes_id, no_id) {
            (Some(y), Some(n)) => {
                let yes_logit = logits[y.0 as usize];
                let no_logit = logits[n.0 as usize];
                // Softmax between yes/no → probability of "yes".
                let max = yes_logit.max(no_logit);
                let yes_exp = (yes_logit - max).exp();
                let no_exp = (no_logit - max).exp();
                (yes_exp / (yes_exp + no_exp)) as f64
            }
            _ => {
                // Fallback: sigmoid of first logit.
                let logit = logits.iter().next().copied().unwrap_or(0.0);
                sigmoid(logit as f64)
            }
        };

        ctx.clear_kv_cache();
        Ok(score)
    }
}

/// Build a reranker prompt for the given query-document pair.
/// Exposed for testing.
#[cfg_attr(not(feature = "llama"), allow(dead_code))]
pub(crate) fn build_prompt(query: &str, document: &str) -> String {
    let q = truncate_str(query, MAX_QUERY_CHARS);
    let d = truncate_str(document, MAX_DOC_CHARS);
    RERANKER_PROMPT_TEMPLATE
        .replace("{query}", q)
        .replace("{document}", d)
}

/// Truncate a string to at most `max_chars` characters (char boundary safe).
#[cfg_attr(not(feature = "llama"), allow(dead_code))]
fn truncate_str(s: &str, max_chars: usize) -> &str {
    if s.len() <= max_chars {
        return s;
    }
    // Find a char boundary at or before max_chars.
    let mut end = max_chars;
    while end > 0 && !s.is_char_boundary(end) {
        end -= 1;
    }
    &s[..end]
}

/// Sigmoid function: 1 / (1 + e^(-x)).
#[cfg_attr(not(feature = "llama"), allow(dead_code))]
fn sigmoid(x: f64) -> f64 {
    1.0 / (1.0 + (-x).exp())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sigmoid_values() {
        assert!((sigmoid(0.0) - 0.5).abs() < 1e-10);
        assert!(sigmoid(10.0) > 0.999);
        assert!(sigmoid(-10.0) < 0.001);
    }

    #[test]
    fn truncate_str_short() {
        assert_eq!(truncate_str("hello", 10), "hello");
    }

    #[test]
    fn truncate_str_exact() {
        assert_eq!(truncate_str("hello", 5), "hello");
    }

    #[test]
    fn truncate_str_long() {
        assert_eq!(truncate_str("hello world", 5), "hello");
    }

    #[test]
    fn truncate_str_unicode() {
        // Korean chars are 3 bytes each in UTF-8.
        let s = "안녕하세요";
        let t = truncate_str(s, 6); // 2 Korean chars = 6 bytes
        assert_eq!(t, "안녕");
    }

    #[test]
    fn build_prompt_contains_query_and_doc() {
        let p = build_prompt("test query", "test document");
        assert!(p.contains("<query>test query</query>"));
        assert!(p.contains("<document>test document</document>"));
        assert!(p.contains("yes"));
        assert!(p.contains("no"));
    }

    #[test]
    fn rerank_empty_docs() {
        let mgr = ModelManager::new(vec![]);
        let reranker = LocalReranker::new(mgr);
        let results = reranker.rerank("query", &[]).unwrap();
        assert!(results.is_empty());
    }

    #[test]
    fn rerank_empty_query() {
        let mgr = ModelManager::new(vec![]);
        let reranker = LocalReranker::new(mgr);
        let results = reranker.rerank("", &["doc1", "doc2"]).unwrap();
        assert_eq!(results.len(), 2);
        assert_eq!(results[0].score, 0.0);
        assert_eq!(results[1].score, 0.0);
    }

    #[test]
    #[cfg(not(feature = "llama"))]
    fn rerank_without_backend() {
        let mgr = ModelManager::new(vec![crate::manager::ModelConfig::reranker(
            std::path::PathBuf::from("/nonexistent.gguf"),
            300,
        )]);
        let reranker = LocalReranker::new(mgr);
        let err = reranker.rerank("query", &["doc"]).unwrap_err();
        assert!(matches!(err, MlError::BackendUnavailable));
    }
}
