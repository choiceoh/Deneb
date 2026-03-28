//! System info command — database health and stats.

use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

pub struct SystemHandler;

impl super::CommandHandler for SystemHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_system(args, config)
    }
}

/// system: Database health and stats.
pub(super) fn cmd_system(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("system", &e),
    };

    let stats: (i64, i64, i64, i64) = conn
        .query_row(
            "SELECT
                (SELECT COUNT(*) FROM projects),
                (SELECT COUNT(*) FROM chunks),
                (SELECT COUNT(*) FROM comm_log),
                (SELECT COUNT(DISTINCT name) FROM tags)",
            [],
            |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?, r.get(3)?)),
        )
        .unwrap_or((0, 0, 0, 0));

    let ver: u32 = conn
        .pragma_query_value(None, "user_version", |r| r.get(0))
        .unwrap_or(0);

    CommandResult::ok(
        "system",
        json!({
            "version": crate::config::VERSION,
            "schema_version": ver,
            "db_path": config.db_path.to_string_lossy(),
            "md_dir": config.md_dir.to_string_lossy(),
            "rerank_mode": config.rerank_mode,
            "inference_backend": config.inference_backend,
            "stats": {
                "projects": stats.0,
                "chunks": stats.1,
                "communications": stats.2,
                "tags": stats.3,
            },
        }),
    )
}
