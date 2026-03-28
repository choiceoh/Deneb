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
        .and_then(|v| v.as_i64());

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

#[cfg(test)]
mod tests {
    use std::path::PathBuf;

    use rusqlite::params;
    use serde_json::json;
    use tempfile::TempDir;

    use super::*;
    use crate::db::schema::init_db;

    fn setup_db() -> Result<(TempDir, VegaConfig), Box<dyn std::error::Error>> {
        let tmp = tempfile::tempdir()?;
        let db_path = tmp.path().join("projects.db");
        let conn = rusqlite::Connection::open(&db_path)?;
        init_db(&conn)?;

        conn.execute(
            "INSERT INTO projects (id, name) VALUES (?1, ?2)",
            params![1_i64, "Timeline Project"],
        )?;
        conn.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![
                1_i64,
                "2026-03-26",
                "owner@deneb.ai",
                "리뷰",
                "타임라인 테스트"
            ],
        )?;

        let cfg = VegaConfig {
            db_path,
            md_dir: PathBuf::from("projects"),
            ..VegaConfig::default()
        };
        Ok((tmp, cfg))
    }

    #[test]
    fn cmd_timeline_resolves_project_by_query() -> Result<(), Box<dyn std::error::Error>> {
        let (_tmp, cfg) = setup_db()?;

        let result = cmd_timeline(&json!({"query":"Timeline"}), &cfg);
        assert!(result.success);
        assert_eq!(
            result.data.get("project_name").and_then(|v| v.as_str()),
            Some("Timeline Project")
        );
        assert_eq!(result.data.get("count").and_then(|v| v.as_u64()), Some(1));
        Ok(())
    }

    #[test]
    fn cmd_timeline_requires_project_hint() {
        let cfg = VegaConfig::default();
        let result = cmd_timeline(&json!({}), &cfg);
        assert!(!result.success);
        assert!(result
            .error
            .as_deref()
            .unwrap_or_default()
            .contains("프로젝트 ID 또는 이름이 필요합니다"));
    }
}
