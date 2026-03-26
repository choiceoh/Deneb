use serde::{Deserialize, Serialize};

/// A single hybrid search result (used for both vector and keyword sources).
/// The `score` field holds the source-specific score (vector similarity or text relevance).
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct HybridResult {
    pub id: String,
    pub path: String,
    pub start_line: u32,
    pub end_line: u32,
    pub source: String,
    pub snippet: String,
    pub score: f64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MergedResult {
    pub path: String,
    pub start_line: u32,
    pub end_line: u32,
    pub score: f64,
    pub snippet: String,
    pub source: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MmrItem {
    pub id: String,
    pub score: f64,
    pub content: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MmrConfig {
    pub enabled: bool,
    pub lambda: f64,
}

impl Default for MmrConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            lambda: 0.7,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct TemporalDecayConfig {
    pub enabled: bool,
    pub half_life_days: f64,
}

impl Default for TemporalDecayConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            half_life_days: 30.0,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MergeParams {
    pub vector: Vec<HybridResult>,
    pub keyword: Vec<HybridResult>,
    pub vector_weight: f64,
    pub text_weight: f64,
    pub mmr: Option<MmrConfig>,
    pub temporal_decay: Option<TemporalDecayConfig>,
    pub now_ms: Option<f64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ExpandedQuery {
    pub original: String,
    pub keywords: Vec<String>,
    pub expanded: String,
}
