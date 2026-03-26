//! Default constants for agent metadata when upstream does not supply them.
//!
//! Mirrors `src/agents/defaults.ts`. Keep in sync.

use serde::{Deserialize, Serialize};

pub const DEFAULT_PROVIDER: &str = "anthropic";
pub const DEFAULT_MODEL: &str = "claude-opus-4-6";
/// Conservative fallback when model metadata is unavailable.
pub const DEFAULT_CONTEXT_TOKENS: u64 = 200_000;

// Per-mode model defaults. Agents can override these per-agent via config.
pub const DEFAULT_THINKING_MODEL: &str = "claude-opus-4-6";
pub const DEFAULT_FAST_MODEL: &str = "claude-sonnet-4-6";
pub const DEFAULT_REASONING_MODEL: &str = "claude-opus-4-6";

/// Model mode identifier for per-agent model selection.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ModelMode {
    Default,
    Thinking,
    Fast,
    Reasoning,
}

/// Per-agent model defaults configuration shape.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentModelDefaults {
    /// Default model for general use. Falls back to DEFAULT_MODEL.
    pub model: Option<String>,
    /// Model for deep thinking/analysis tasks.
    pub thinking_model: Option<String>,
    /// Model for fast, lightweight responses (e.g., /btw).
    pub fast_model: Option<String>,
    /// Model for structured reasoning with explicit CoT.
    pub reasoning_model: Option<String>,
}

/// Resolve the model ID for a given agent and mode.
/// Priority: agent-level override -> global defaults -> hardcoded constants.
pub fn resolve_agent_model<'a>(
    mode: ModelMode,
    agent_defaults: Option<&'a AgentModelDefaults>,
    is_model_available: Option<&dyn Fn(&str) -> bool>,
) -> &'a str {
    let agent_model = agent_defaults.and_then(|d| match mode {
        ModelMode::Default => d.model.as_deref(),
        ModelMode::Thinking => d.thinking_model.as_deref(),
        ModelMode::Fast => d.fast_model.as_deref(),
        ModelMode::Reasoning => d.reasoning_model.as_deref(),
    });

    if let Some(model) = agent_model {
        // Auto-revert: if the model is not available, fall back.
        if let Some(check) = is_model_available {
            if !check(model) {
                return DEFAULT_MODEL;
            }
        }
        return model;
    }

    // Fall back to global default for this mode.
    match mode {
        ModelMode::Default => DEFAULT_MODEL,
        ModelMode::Thinking => DEFAULT_THINKING_MODEL,
        ModelMode::Fast => DEFAULT_FAST_MODEL,
        ModelMode::Reasoning => DEFAULT_REASONING_MODEL,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_mode_returns_default_model() {
        assert_eq!(resolve_agent_model(ModelMode::Default, None, None), DEFAULT_MODEL);
    }

    #[test]
    fn fast_mode_returns_fast_model() {
        assert_eq!(resolve_agent_model(ModelMode::Fast, None, None), DEFAULT_FAST_MODEL);
    }

    #[test]
    fn agent_override_takes_precedence() {
        let defaults = AgentModelDefaults {
            model: Some("custom-model".to_string()),
            ..Default::default()
        };
        assert_eq!(
            resolve_agent_model(ModelMode::Default, Some(&defaults), None),
            "custom-model"
        );
    }

    #[test]
    fn unavailable_model_falls_back() {
        let defaults = AgentModelDefaults {
            model: Some("unavailable-model".to_string()),
            ..Default::default()
        };
        let check = |_: &str| false;
        assert_eq!(
            resolve_agent_model(ModelMode::Default, Some(&defaults), Some(&check)),
            DEFAULT_MODEL
        );
    }

    #[test]
    fn available_model_is_kept() {
        let defaults = AgentModelDefaults {
            thinking_model: Some("my-thinker".to_string()),
            ..Default::default()
        };
        let check = |_: &str| true;
        assert_eq!(
            resolve_agent_model(ModelMode::Thinking, Some(&defaults), Some(&check)),
            "my-thinker"
        );
    }
}
