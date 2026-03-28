//! Upgrade command — schema migrations and FTS rebuild.

use serde_json::{json, Value};

use crate::config::VegaConfig;
use crate::db::schema::init_db;

use super::{open_db, CommandResult};

pub struct UpgradeHandler;

impl super::CommandHandler for UpgradeHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_upgrade(args, config)
    }
}

/// upgrade: Run schema migrations and FTS rebuild.
pub(super) fn cmd_upgrade(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("upgrade", &e),
    };

    let ver_before: u32 = conn
        .pragma_query_value(None, "user_version", |r| r.get(0))
        .unwrap_or(0);

    if let Err(e) = init_db(&conn) {
        return CommandResult::err("upgrade", &format!("스키마 업그레이드 실패: {}", e));
    }

    if let Err(e) = crate::db::schema::rebuild_fts(&conn) {
        return CommandResult::err("upgrade", &format!("FTS 리빌드 실패: {}", e));
    }

    let ver_after: u32 = conn
        .pragma_query_value(None, "user_version", |r| r.get(0))
        .unwrap_or(0);

    CommandResult::ok(
        "upgrade",
        json!({
            "schema_before": ver_before,
            "schema_after": ver_after,
            "fts_rebuilt": true,
        }),
    )
}
