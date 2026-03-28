//! Timeline command — project communication timeline.

use rusqlite::params;
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{find_project_id, open_db, CommandResult};

pub struct TimelineHandler;

impl super::CommandHandler for TimelineHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_timeline(args, config)
    }
}

/// timeline: Project communication timeline.
pub(super) fn cmd_timeline(args: &Value, config: &VegaConfig) -> CommandResult {
    let project_id = args
        .get("id")
        .or_else(|| args.get("project_id"))
        .and_then(|v| v.as_i64());

    let project_id = match project_id {
        Some(id) => id,
        None => {
            let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
            match find_project_id(config, query) {
                Some(id) => id,
                None => {
                    return CommandResult::err("timeline", "프로젝트 ID 또는 이름이 필요합니다")
                }
            }
        }
    };

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("timeline", &e),
    };

    let name: String = conn
        .query_row(
            "SELECT name FROM projects WHERE id=?1",
            params![project_id],
            |r| r.get(0),
        )
        .unwrap_or_default();

    let mut stmt = match conn
        .prepare("SELECT log_date, sender, subject, summary FROM comm_log WHERE project_id=?1 ORDER BY log_date DESC")
    {
        Ok(s) => s,
        Err(e) => return CommandResult::err("timeline", &format!("타임라인 쿼리 실패: {e}")),
    };

    let entries: Vec<Value> = match stmt.query_map(params![project_id], |r| {
        Ok(json!({
            "date": r.get::<_, Option<String>>(0)?,
            "sender": r.get::<_, Option<String>>(1)?,
            "subject": r.get::<_, Option<String>>(2)?,
            "summary": r.get::<_, Option<String>>(3)?,
        }))
    }) {
        Ok(rows) => rows.filter_map(|r| r.ok()).collect(),
        Err(e) => return CommandResult::err("timeline", &format!("타임라인 쿼리 실패: {e}")),
    };

    CommandResult::ok(
        "timeline",
        json!({
            "project_id": project_id,
            "project_name": name,
            "entries": entries,
            "count": entries.len(),
        }),
    )
}
