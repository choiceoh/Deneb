//! List command — list all projects.

use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

pub struct ListHandler;

impl super::CommandHandler for ListHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_list(args, config)
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
