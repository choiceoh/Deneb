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

    CommandResult::err("embed", "임베딩 백엔드가 설정되지 않았습니다 (VEGA_INFERENCE=sglang 권장)")
}
