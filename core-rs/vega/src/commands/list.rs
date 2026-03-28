//! List command — list all projects.

use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

pub struct ListHandler;

impl super::CommandHandler for ListHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_list(args, config)
    }

    fn compact_result(&self, data: &Value) -> Value {
        json!({
            "total": data.get("projects").and_then(|v| v.as_array()).map(|a| a.len()),
            "projects": data.get("projects").and_then(|v| v.as_array()).map(|arr| {
                arr.iter().map(|p| json!({
                    "id": p.get("id"), "name": p.get("name"), "status": p.get("status"),
                })).collect::<Vec<_>>()
            }),
        })
    }

    fn ai_hints(&self, data: &Value) -> Vec<Value> {
        let count = data
            .get("projects")
            .and_then(|v| v.as_array())
            .map(|a| a.len())
            .unwrap_or(0);
        vec![json!({"situation": "project_list",
            "guide": format!("{}개 프로젝트 목록입니다. 상세는 brief <ID>로 확인하세요.", count)})]
    }
}

/// list: List all projects.
pub(super) fn cmd_list(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("list", &e),
    };

    let mut stmt = match conn.prepare(
        "SELECT p.id, p.name, p.client, p.status, p.person_internal, p.capacity,
                    (SELECT COUNT(*) FROM chunks WHERE project_id=p.id) as chunks,
                    (SELECT COUNT(*) FROM comm_log WHERE project_id=p.id) as comms
             FROM projects p ORDER BY p.id",
    ) {
        Ok(s) => s,
        Err(e) => return CommandResult::err("list", &format!("프로젝트 목록 쿼리 실패: {e}")),
    };

    let projects: Vec<Value> = match stmt.query_map([], |r| {
        Ok(json!({
            "id": r.get::<_, i64>(0)?,
            "name": r.get::<_, Option<String>>(1)?,
            "client": r.get::<_, Option<String>>(2)?,
            "status": r.get::<_, Option<String>>(3)?,
            "person": r.get::<_, Option<String>>(4)?,
            "capacity": r.get::<_, Option<String>>(5)?,
            "chunks": r.get::<_, i64>(6)?,
            "comms": r.get::<_, i64>(7)?,
        }))
    }) {
        Ok(rows) => rows.filter_map(|r| r.ok()).collect(),
        Err(e) => return CommandResult::err("list", &format!("프로젝트 목록 쿼리 실패: {e}")),
    };

    CommandResult::ok("list", json!({ "projects": projects }))
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
            "INSERT INTO projects (id, name, client, status, person_internal, capacity)
             VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            params![1_i64, "Alpha", "ClientA", "진행중", "Kim", "80%"],
        )?;
        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (?1, ?2, ?3, ?4)",
            params![1_i64, "상태", "본문", "status"],
        )?;
        conn.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
             VALUES (?1, ?2, ?3, ?4, ?5)",
            params![1_i64, "2026-03-20", "tester@deneb.ai", "업데이트", "정상"],
        )?;

        let cfg = VegaConfig {
            db_path,
            md_dir: PathBuf::from("projects"),
            ..VegaConfig::default()
        };
        Ok((tmp, cfg))
    }

    #[test]
    fn cmd_list_returns_project_counts() -> Result<(), Box<dyn std::error::Error>> {
        let (_tmp, cfg) = setup_db()?;

        let result = cmd_list(&json!({}), &cfg);
        assert!(result.success);
        let projects = result
            .data
            .get("projects")
            .and_then(|v| v.as_array())
            .expect("projects should be an array");
        assert_eq!(projects.len(), 1);
        assert_eq!(projects[0].get("chunks").and_then(|v| v.as_i64()), Some(1));
        assert_eq!(projects[0].get("comms").and_then(|v| v.as_i64()), Some(1));
        Ok(())
    }
}
