//! Memory search algorithms — Rust port of `src/memory/` TypeScript modules.
//!
//! Pure computational functions for:
//! - Cosine similarity (SIMD-accelerated on `x86_64`)
//! - BM25 rank-to-score conversion
//! - FTS query building
//! - Temporal decay scoring
//! - MMR diversity re-ranking
//! - Multilingual query expansion / keyword extraction
//! - Hybrid result merging pipeline

pub mod bm25;
pub mod cosine;
pub mod simd;
pub mod fts;
pub mod merge;
pub mod mmr;
pub mod napi;
pub mod query_expansion;
pub mod stop_words;
pub mod temporal_decay;
pub mod types;
