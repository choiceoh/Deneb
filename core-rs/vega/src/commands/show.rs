//! Show command — detailed project info.

use rusqlite::{params, Connection};
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{CommandContext, CommandResult};

pub struct ShowHandler;

impl super::CommandHandler for ShowHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_show(args, config)
    }

    fn compact_result(&self, data: &Value) -> Value {
        json!({
            "id": data.get("id").or(data.get("project").and_then(|p| p.get("id"))),
            "name": data.get("name").or(data.get("project").and_then(|p| p.get("name"))),
            "status": data.get("status").or(data.get("project").and_then(|p| p.get("status"))),
            "client": data.get("client").or(data.get("project").and_then(|p| p.get("client"))),
            "person_internal": data.get("person_internal").or(data.get("project").and_then(|p| p.get("person_internal"))),
            "section_count": data.get("sections").and_then(|v| v.as_array()).map(|a| a.len()),
            "comm_count": data.get("communications").and_then(|v| v.as_array()).map(|a| a.len()),
        })
    }

    fn ai_hints(&self, data: &Value) -> Vec<Value> {
        let pid = data
            .get("id")
            .or(data.get("project").and_then(|p| p.get("id")))
            .and_then(|v| v.as_i64())
            .unwrap_or(0);
        if pid > 0 {
            vec![json!({"situation": "show_detail",
                "guide": format!("프로젝트 상세입니다. 요약: brief {}, 이력: timeline {}.", pid, pid)})]
        } else {
            vec![]
        }
    }

    fn build_bundle(&self, data: &Value, conn: Option<&Connection>) -> Value {
        let conn = match conn {
            Some(c) => c,
            None => return json!({}),
        };
        let mut bundle = json!({});
        let client = data
            .get("project")
            .and_then(|p| p.get("client"))
            .and_then(|v| v.as_str())
            .or_else(|| data.get("client").and_then(|v| v.as_str()))
            .unwrap_or("");
        let show_pid = data
            .get("project")
            .and_then(|p| p.get("id"))
            .and_then(|v| v.as_i64())
            .or_else(|| data.get("id").and_then(|v| v.as_i64()));
        if !client.is_empty() {
            if let (Some(pid), Some(first_word)) = (show_pid, client.split_whitespace().next()) {
                if let Ok(mut stmt) = conn.prepare(
                    "SELECT id, name, status FROM projects WHERE client LIKE ?1 AND id != ?2 LIMIT 3",
                ) {
                    let pattern = format!("%{}%", crate::utils::escape_like(first_word));
                    if let Ok(rows) = stmt.query_map(rusqlite::params![pattern, pid], |r| {
                        Ok(json!({"id": r.get::<_, i64>(0)?, "name": r.get::<_, String>(1)?, "status": r.get::<_, String>(2)?}))
                    }) {
                        let related: Vec<Value> = rows.flatten().collect();
                        if !related.is_empty() {
                            bundle["same_client_projects"] = json!(related);
                        }
                    }
                }
            }
        }
        bundle
    }

    fn summary(&self, data: &Value) -> String {
        let name = data
            .get("project")
            .and_then(|p| p.get("name"))
            .and_then(|v| v.as_str())
            .unwrap_or("?");
        format!("프로젝트 상세: {}", name)
    }
}

/// show: Display detailed project info.
pub(super) fn cmd_show(args: &Value, config: &VegaConfig) -> CommandResult {
    let ctx = match CommandContext::new(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("show", &e),
    };

    let project_id = args
        .get("id")
        .or_else(|| args.get("project_id"))
        .and_then(|v| v.as_i64());

    let project_id = match project_id {
        Some(id) => id,
        None => {
            let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
            if query.is_empty() {
                return CommandResult::err("show", "프로젝트 ID 또는 이름이 필요합니다");
            }
            match ctx.find_project(query) {
                Some(id) => id,
                None => return CommandResult::err("show", &format!("프로젝트 '{}' 없음", query)),
            }
        }
    };

    let proj = ctx.conn.query_row(
        "SELECT id, name, client, status, capacity, biz_type, person_internal, person_external, partner
         FROM projects WHERE id=?1",
        params![project_id],
        |r| {
            Ok(json!({
                "id": r.get::<_, i64>(0)?,
                "name": r.get::<_, Option<String>>(1)?,
                "client": r.get::<_, Option<String>>(2)?,
                "status": r.get::<_, Option<String>>(3)?,
                "capacity": r.get::<_, Option<String>>(4)?,
                "biz_type": r.get::<_, Option<String>>(5)?,
                "person_internal": r.get::<_, Option<String>>(6)?,
                "person_external": r.get::<_, Option<String>>(7)?,
                "partner": r.get::<_, Option<String>>(8)?,
            }))
        },
    );

    let proj = match proj {
        Ok(p) => p,
        Err(_) => return CommandResult::err("show", &format!("프로젝트 ID {} 없음", project_id)),
    };

    // Chunks
    let mut stmt = match ctx.conn
        .prepare("SELECT section_heading, content, chunk_type, entry_date FROM chunks WHERE project_id=?1 ORDER BY id")
    {
        Ok(s) => s,
        Err(e) => return CommandResult::err("show", &format!("청크 쿼리 준비 실패: {e}")),
    };
    let chunks: Vec<Value> = match stmt.query_map(params![project_id], |r| {
        Ok(json!({
            "heading": r.get::<_, Option<String>>(0)?,
            "content": r.get::<_, Option<String>>(1)?,
            "type": r.get::<_, Option<String>>(2)?,
            "date": r.get::<_, Option<String>>(3)?,
        }))
    }) {
        Ok(rows) => rows.filter_map(|r| r.ok()).collect(),
        Err(e) => return CommandResult::err("show", &format!("청크 쿼리 실행 실패: {e}")),
    };

    // Tags
    let mut stmt = match ctx.conn.prepare(
        "SELECT DISTINCT t.name FROM tags t
             JOIN chunk_tags ct ON ct.tag_id = t.id
             JOIN chunks c ON c.id = ct.chunk_id
             WHERE c.project_id = ?1",
    ) {
        Ok(s) => s,
        Err(e) => return CommandResult::err("show", &format!("태그 쿼리 준비 실패: {e}")),
    };
    let tags: Vec<String> = match stmt.query_map(params![project_id], |r| r.get(0)) {
        Ok(rows) => rows.filter_map(|r| r.ok()).collect(),
        Err(e) => return CommandResult::err("show", &format!("태그 쿼리 실행 실패: {e}")),
    };

    // Recent comms
    let mut stmt = match ctx.conn
        .prepare("SELECT log_date, sender, subject, summary FROM comm_log WHERE project_id=?1 ORDER BY log_date DESC LIMIT 10")
    {
        Ok(s) => s,
        Err(e) => return CommandResult::err("show", &format!("통신 쿼리 준비 실패: {e}")),
    };
    let comms: Vec<Value> = match stmt.query_map(params![project_id], |r| {
        Ok(json!({
            "date": r.get::<_, Option<String>>(0)?,
            "sender": r.get::<_, Option<String>>(1)?,
            "subject": r.get::<_, Option<String>>(2)?,
            "summary": r.get::<_, Option<String>>(3)?,
        }))
    }) {
        Ok(rows) => rows.filter_map(|r| r.ok()).collect(),
        Err(e) => return CommandResult::err("show", &format!("통신 쿼리 실행 실패: {e}")),
    };

    CommandResult::ok(
        "show",
        json!({
            "project": proj,
            "sections": chunks,
            "tags": tags,
            "communications": comms,
        }),
    )
}
