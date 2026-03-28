//! Shared types for the memory search pipeline (results, configs, merge params).

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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_mmr_config_default() {
        let cfg = MmrConfig::default();
        assert!(!cfg.enabled);
        assert!((cfg.lambda - 0.7).abs() < f64::EPSILON);
    }

    #[test]
    fn test_temporal_decay_config_default() {
        let cfg = TemporalDecayConfig::default();
        assert!(!cfg.enabled);
        assert!((cfg.half_life_days - 30.0).abs() < f64::EPSILON);
    }

    #[test]
    fn test_hybrid_result_serde_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
        let result = HybridResult {
            id: "doc1".into(),
            path: "memory/2026-01-15.md".into(),
            start_line: 1,
            end_line: 10,
            source: "vector".into(),
            snippet: "test snippet".into(),
            score: 0.95,
        };
        let json = serde_json::to_string(&result)?;
        assert!(json.contains("\"startLine\":1"));
        assert!(json.contains("\"endLine\":10"));

        let deserialized: HybridResult = serde_json::from_str(&json)?;
        assert_eq!(deserialized.id, "doc1");
        assert_eq!(deserialized.start_line, 1);
        assert!((deserialized.score - 0.95).abs() < f64::EPSILON);
        Ok(())
    }

    #[test]
    fn test_merged_result_serde_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
        let result = MergedResult {
            path: "notes/topic.md".into(),
            start_line: 5,
            end_line: 15,
            score: 0.8,
            snippet: "merged snippet".into(),
            source: "hybrid".into(),
        };
        let json = serde_json::to_string(&result)?;
        let deserialized: MergedResult = serde_json::from_str(&json)?;
        assert_eq!(deserialized.path, "notes/topic.md");
        assert!((deserialized.score - 0.8).abs() < f64::EPSILON);
        Ok(())
    }

    #[test]
    fn test_merge_params_serde() -> Result<(), Box<dyn std::error::Error>> {
        let params = MergeParams {
            vector: vec![],
            keyword: vec![],
            vector_weight: 0.6,
            text_weight: 0.4,
            mmr: Some(MmrConfig::default()),
            temporal_decay: None,
            now_ms: Some(1700000000000.0),
        };
        let json = serde_json::to_string(&params)?;
        assert!(json.contains("\"vectorWeight\":0.6"));
        assert!(json.contains("\"textWeight\":0.4"));

        let deserialized: MergeParams = serde_json::from_str(&json)?;
        assert!((deserialized.vector_weight - 0.6).abs() < f64::EPSILON);
        assert!(deserialized.mmr.is_some());
        assert!(deserialized.temporal_decay.is_none());
        Ok(())
    }

    #[test]
    fn test_expanded_query_serde() -> Result<(), Box<dyn std::error::Error>> {
        let query = ExpandedQuery {
            original: "test query".into(),
            keywords: vec!["test".into(), "query".into()],
            expanded: "test OR query".into(),
        };
        let json = serde_json::to_string(&query)?;
        let deserialized: ExpandedQuery = serde_json::from_str(&json)?;
        assert_eq!(deserialized.original, "test query");
        assert_eq!(deserialized.keywords.len(), 2);
        Ok(())
    }

    #[test]
    fn test_mmr_item_serde() -> Result<(), Box<dyn std::error::Error>> {
        let item = MmrItem {
            id: "item1".into(),
            score: 0.85,
            content: "some content".into(),
        };
        let json = serde_json::to_string(&item)?;
        let deserialized: MmrItem = serde_json::from_str(&json)?;
        assert_eq!(deserialized.id, "item1");
        assert!((deserialized.score - 0.85).abs() < f64::EPSILON);
        Ok(())
    }
}
