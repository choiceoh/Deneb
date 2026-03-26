//! Token usage normalization across multiple provider SDKs.
//!
//! Mirrors `src/agents/usage.ts`. Keep in sync.
//!
//! Handles the many different field naming conventions used by various LLM
//! providers (Anthropic, OpenAI, Google, Moonshot/Kimi, etc.) and normalizes
//! them into a single canonical form.

use serde::{Deserialize, Serialize};

/// Raw usage data as reported by various providers. Supports all known field
/// naming conventions. Deserialized from JSON with flexible field matching.
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct UsageLike {
    // Canonical fields.
    pub input: Option<f64>,
    pub output: Option<f64>,
    #[serde(alias = "cache_read")]
    pub cache_read: Option<f64>,
    #[serde(alias = "cache_write")]
    pub cache_write: Option<f64>,
    pub total: Option<f64>,
    // Common alternates across providers/SDKs.
    #[serde(alias = "inputTokens")]
    pub input_tokens: Option<f64>,
    #[serde(alias = "outputTokens")]
    pub output_tokens: Option<f64>,
    #[serde(alias = "promptTokens")]
    pub prompt_tokens: Option<f64>,
    #[serde(alias = "completionTokens")]
    pub completion_tokens: Option<f64>,
    // Anthropic-specific.
    pub cache_read_input_tokens: Option<f64>,
    pub cache_creation_input_tokens: Option<f64>,
    // Moonshot/Kimi.
    pub cached_tokens: Option<f64>,
    // Kimi K2.
    pub prompt_tokens_details: Option<PromptTokensDetails>,
    // Total alternates.
    #[serde(alias = "totalTokens")]
    pub total_tokens: Option<f64>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct PromptTokensDetails {
    pub cached_tokens: Option<f64>,
}

/// Normalized usage with canonical field names.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct NormalizedUsage {
    pub input: Option<f64>,
    pub output: Option<f64>,
    pub cache_read: Option<f64>,
    pub cache_write: Option<f64>,
    pub total: Option<f64>,
}

/// Full usage snapshot with token counts and cost breakdown.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AssistantUsageSnapshot {
    pub input: f64,
    pub output: f64,
    pub cache_read: f64,
    pub cache_write: f64,
    pub total_tokens: f64,
    pub cost: UsageCost,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UsageCost {
    pub input: f64,
    pub output: f64,
    pub cache_read: f64,
    pub cache_write: f64,
    pub total: f64,
}

/// Create a zero-valued usage snapshot.
pub fn make_zero_usage_snapshot() -> AssistantUsageSnapshot {
    AssistantUsageSnapshot {
        input: 0.0,
        output: 0.0,
        cache_read: 0.0,
        cache_write: 0.0,
        total_tokens: 0.0,
        cost: UsageCost {
            input: 0.0,
            output: 0.0,
            cache_read: 0.0,
            cache_write: 0.0,
            total: 0.0,
        },
    }
}

/// Filter non-finite numbers to None.
fn as_finite(value: Option<f64>) -> Option<f64> {
    value.filter(|v| v.is_finite())
}

/// Check if a normalized usage has any nonzero finite values.
pub fn has_nonzero_usage(usage: Option<&NormalizedUsage>) -> bool {
    let u = match usage {
        Some(u) => u,
        None => return false,
    };
    [u.input, u.output, u.cache_read, u.cache_write, u.total]
        .iter()
        .any(|v| matches!(v, Some(n) if n.is_finite() && *n > 0.0))
}

/// Normalize raw provider usage data into canonical form.
pub fn normalize_usage(raw: &UsageLike) -> Option<NormalizedUsage> {
    let raw_input = as_finite(raw.input)
        .or(as_finite(raw.input_tokens))
        .or(as_finite(raw.prompt_tokens));
    // Clamp negative input (some providers pre-subtract cached_tokens).
    let input = raw_input.map(|v| if v < 0.0 { 0.0 } else { v });

    let output = as_finite(raw.output)
        .or(as_finite(raw.output_tokens))
        .or(as_finite(raw.completion_tokens));

    let cache_read = as_finite(raw.cache_read)
        .or(as_finite(raw.cache_read_input_tokens))
        .or(as_finite(raw.cached_tokens))
        .or(as_finite(
            raw.prompt_tokens_details
                .as_ref()
                .and_then(|d| d.cached_tokens),
        ));

    let cache_write = as_finite(raw.cache_write).or(as_finite(raw.cache_creation_input_tokens));

    let total = as_finite(raw.total).or(as_finite(raw.total_tokens));

    if input.is_none()
        && output.is_none()
        && cache_read.is_none()
        && cache_write.is_none()
        && total.is_none()
    {
        return None;
    }

    Some(NormalizedUsage {
        input,
        output,
        cache_read,
        cache_write,
        total,
    })
}

/// Derive prompt tokens from input + cache read + cache write.
pub fn derive_prompt_tokens(
    input: Option<f64>,
    cache_read: Option<f64>,
    cache_write: Option<f64>,
) -> Option<f64> {
    let sum = input.unwrap_or(0.0) + cache_read.unwrap_or(0.0) + cache_write.unwrap_or(0.0);
    if sum > 0.0 {
        Some(sum)
    } else {
        None
    }
}

/// Derive session total tokens (prompt/context snapshot, excludes output).
pub fn derive_session_total_tokens(
    usage: Option<&NormalizedUsage>,
    prompt_tokens_override: Option<f64>,
) -> Option<f64> {
    let has_override = prompt_tokens_override
        .filter(|v| v.is_finite() && *v > 0.0)
        .is_some();

    if usage.is_none() && !has_override {
        return None;
    }

    let prompt_tokens = if has_override {
        prompt_tokens_override
    } else {
        usage.and_then(|u| derive_prompt_tokens(u.input, u.cache_read, u.cache_write))
    };

    prompt_tokens.filter(|v| v.is_finite() && *v > 0.0)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn normalize_anthropic_usage() {
        let raw = UsageLike {
            input: Some(100.0),
            output: Some(50.0),
            cache_read_input_tokens: Some(200.0),
            cache_creation_input_tokens: Some(10.0),
            ..Default::default()
        };
        let result = normalize_usage(&raw).unwrap();
        assert_eq!(result.input, Some(100.0));
        assert_eq!(result.output, Some(50.0));
        assert_eq!(result.cache_read, Some(200.0));
        assert_eq!(result.cache_write, Some(10.0));
    }

    #[test]
    fn normalize_openai_usage() {
        let raw = UsageLike {
            prompt_tokens: Some(500.0),
            completion_tokens: Some(200.0),
            total_tokens: Some(700.0),
            ..Default::default()
        };
        let result = normalize_usage(&raw).unwrap();
        assert_eq!(result.input, Some(500.0));
        assert_eq!(result.output, Some(200.0));
        assert_eq!(result.total, Some(700.0));
    }

    #[test]
    fn normalize_negative_input_clamped() {
        let raw = UsageLike {
            input: Some(-50.0),
            output: Some(100.0),
            ..Default::default()
        };
        let result = normalize_usage(&raw).unwrap();
        assert_eq!(result.input, Some(0.0));
    }

    #[test]
    fn normalize_empty_returns_none() {
        let raw = UsageLike::default();
        assert!(normalize_usage(&raw).is_none());
    }

    #[test]
    fn normalize_nan_ignored() {
        let raw = UsageLike {
            input: Some(f64::NAN),
            output: Some(f64::INFINITY),
            ..Default::default()
        };
        assert!(normalize_usage(&raw).is_none());
    }

    #[test]
    fn has_nonzero_usage_true() {
        let usage = NormalizedUsage {
            input: Some(100.0),
            ..Default::default()
        };
        assert!(has_nonzero_usage(Some(&usage)));
    }

    #[test]
    fn has_nonzero_usage_false() {
        let usage = NormalizedUsage {
            input: Some(0.0),
            output: Some(0.0),
            ..Default::default()
        };
        assert!(!has_nonzero_usage(Some(&usage)));
        assert!(!has_nonzero_usage(None));
    }

    #[test]
    fn derive_prompt_tokens_basic() {
        assert_eq!(
            derive_prompt_tokens(Some(100.0), Some(200.0), Some(10.0)),
            Some(310.0)
        );
        assert_eq!(derive_prompt_tokens(None, None, None), None);
        assert_eq!(derive_prompt_tokens(Some(0.0), Some(0.0), Some(0.0)), None);
    }

    #[test]
    fn derive_session_total_with_override() {
        let result = derive_session_total_tokens(None, Some(500.0));
        assert_eq!(result, Some(500.0));
    }

    #[test]
    fn derive_session_total_from_usage() {
        let usage = NormalizedUsage {
            input: Some(100.0),
            cache_read: Some(200.0),
            cache_write: Some(10.0),
            ..Default::default()
        };
        let result = derive_session_total_tokens(Some(&usage), None);
        assert_eq!(result, Some(310.0));
    }

    #[test]
    fn derive_session_total_empty() {
        assert_eq!(derive_session_total_tokens(None, None), None);
    }

    #[test]
    fn make_zero_snapshot() {
        let snap = make_zero_usage_snapshot();
        assert_eq!(snap.input, 0.0);
        assert_eq!(snap.total_tokens, 0.0);
        assert_eq!(snap.cost.total, 0.0);
    }
}
