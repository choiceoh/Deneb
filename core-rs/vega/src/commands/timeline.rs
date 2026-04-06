//! Timeline command — project communication timeline.

use rusqlite::params;
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{CommandContext, CommandResult};

pub struct TimelineHandler;

impl super::CommandHandler for TimelineHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_timeline(args, config)
    }

    fn ai_hints(&self, data: &Value) -> Vec<Value> {
        let _ = data;
        vec![json!({"situation": "timeline_view",
            "guide": "이력/일정입니다. 시간순으로 핵심 이벤트를 요약하세요."})]
    }
}

/// timeline: Project communication timeline.
pub(super) fn cmd_timeline(args: &Value, config: &VegaConfig) -> CommandResult {
    let ctx = match CommandContext::new(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("timeline", &e),
    };

    let project_id = args
        .get("id")
        .or_else(|| args.get("project_id"))
        .and_then(serde_json::Value::as_i64);

    let project_id = match project_id {
        Some(id) => id,
        None => {
            let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
            match ctx.find_project(query) {
                Some(id) => id,
                None => {
                    return CommandResult::err("timeline", "프로젝트 ID 또는 이름이 필요합니다")
                }
            }
        }
    };

    let name: String = ctx
        .conn
        .query_row(
            "SELECT name FROM projects WHERE id=?1",
            params![project_id],
            |r| r.get(0),
        )
        .unwrap_or_default();

    let entries = match ctx.query_project_rows(
        project_id,
        "SELECT log_date AS date, sender, subject, summary
         FROM comm_log WHERE project_id=?1 ORDER BY log_date DESC",
    ) {
        Ok(rows) => rows,
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
