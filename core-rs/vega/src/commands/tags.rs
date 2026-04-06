//! Tags command — list all tags with project counts.

use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

pub struct TagsHandler;

impl super::CommandHandler for TagsHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_tags(args, config)
    }
}

/// tags: List all tags with project counts.
pub(super) fn cmd_tags(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("tags", &e),
    };

    let mut stmt = match conn.prepare(
        "SELECT t.name, COUNT(DISTINCT c.project_id) as cnt
             FROM tags t
             JOIN chunk_tags ct ON ct.tag_id = t.id
             JOIN chunks c ON c.id = ct.chunk_id
             GROUP BY t.name ORDER BY t.name",
    ) {
        Ok(s) => s,
        Err(e) => return CommandResult::err("tags", &format!("태그 쿼리 실패: {e}")),
    };

    let tags: Vec<Value> = match stmt.query_map([], |r| {
        Ok(json!({
            "name": r.get::<_, String>(0)?,
            "project_count": r.get::<_, i64>(1)?,
        }))
    }) {
        Ok(rows) => rows.filter_map(std::result::Result::ok).collect(),
        Err(e) => return CommandResult::err("tags", &format!("태그 쿼리 실패: {e}")),
    };

    CommandResult::ok("tags", json!({ "tags": tags }))
}
