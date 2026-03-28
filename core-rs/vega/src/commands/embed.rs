//! Embed command — generate embeddings for chunks.

use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::CommandResult;

pub struct EmbedHandler;

impl super::CommandHandler for EmbedHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_embed(args, config)
    }
}

/// embed: Generate embeddings for chunks.
/// In sglang mode, embedding is handled by the Go gateway via SGLang HTTP API.
/// This Rust-side command is retained for backward compatibility but returns
/// a message directing to the Go-side embed pipeline.
#[allow(unused_variables)]
pub(super) fn cmd_embed(_args: &Value, config: &VegaConfig) -> CommandResult {
    if config.has_sglang() {
        return CommandResult::ok(
            "embed",
            json!({
                "message": "SGLang 모드: 임베딩은 Go 게이트웨이에서 SGLang HTTP API로 처리됩니다.",
                "backend": "sglang",
            }),
        );
    }

    CommandResult::err(
        "embed",
        "임베딩 백엔드가 설정되지 않았습니다 (VEGA_INFERENCE=sglang 권장)",
    )
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::*;

    #[test]
    fn cmd_embed_returns_sglang_guidance_when_backend_is_sglang() {
        let config = VegaConfig::default();

        let result = cmd_embed(&json!({}), &config);
        assert!(result.success);
        assert_eq!(result.command, "embed");
        assert_eq!(
            result.data.get("backend").and_then(|v| v.as_str()),
            Some("sglang")
        );
    }

    #[test]
    fn cmd_embed_fails_when_backend_is_not_sglang() {
        let config = VegaConfig {
            inference_backend: "sqlite_only".to_string(),
            ..VegaConfig::default()
        };

        let result = cmd_embed(&json!({}), &config);
        assert!(!result.success);
        assert_eq!(result.command, "embed");
        assert!(result
            .error
            .as_deref()
            .unwrap_or_default()
            .contains("임베딩 백엔드가 설정되지 않았습니다"));
    }
}
