use rusqlite::{params, Connection};
use serde_json::{json, Value};

use super::{open_db, CommandResult};
use crate::config::VegaConfig;

/// Person query: find all projects and communications for a given person.
/// Args: { "name": "홍길동" }
pub fn cmd_person(args: &Value, config: &VegaConfig) -> CommandResult {
    let name = match args.get("name").and_then(|v| v.as_str()) {
        Some(n) => n.trim(),
        None => return CommandResult::err("person", "이름을 입력해주세요 (name 필수)"),
    };

    if name.is_empty() {
        return CommandResult::err("person", "이름을 입력해주세요");
    }

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("person", &e),
    };

    // 1) Find projects where this person appears as internal personnel
    let internal_projects = match find_person_projects(&conn, name, "person") {
        Ok(v) => v,
        Err(e) => return CommandResult::err("person", &e),
    };

    // 2) Find projects where this person appears as external contact
    let external_projects = match find_person_projects(&conn, name, "external_person") {
        Ok(v) => v,
        Err(e) => return CommandResult::err("person", &e),
    };

    // 3) Find communication logs mentioning this person
    let comm_logs = match find_person_comm_logs(&conn, name) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("person", &e),
    };

    // 4) Find action items assigned to this person
    let actions = match find_person_actions(&conn, name) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("person", &e),
    };

    let total_projects = internal_projects.len() + external_projects.len();

    CommandResult::ok(
        "person",
        json!({
            "name": name,
            "total_projects": total_projects,
            "internal_projects": internal_projects,
            "external_projects": external_projects,
            "comm_logs": comm_logs,
            "actions": actions,
            "summary": format!(
                "{}: 내부 {}건, 외부 {}건, 소통 {}건, 액션 {}건",
                name,
                internal_projects.len(),
                external_projects.len(),
                comm_logs.len(),
                actions.len()
            ),
        }),
    )
}

/// Find projects where a person is referenced in chunks of the given type.
fn find_person_projects(
    conn: &Connection,
    name: &str,
    chunk_type: &str,
) -> Result<Vec<Value>, String> {
    let sql = r#"
        SELECT DISTINCT p.id, p.name, p.status, c.content
        FROM chunks c
        JOIN projects p ON p.id = c.project_id
        WHERE c.chunk_type = ?1
          AND c.content LIKE ?2
        ORDER BY p.name
    "#;

    let pattern = format!("%{}%", name);
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map(params![chunk_type, pattern], |row| {
            let id: i64 = row.get(0)?;
            let proj_name: String = row.get(1)?;
            let status: String = row.get(2)?;
            let content: String = row.get(3)?;
            Ok((id, proj_name, status, content))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut items = Vec::new();
    for (id, proj_name, status, content) in rows.flatten() {
        items.push(json!({
            "project_id": id,
            "project_name": proj_name,
            "status": status,
            "role": content.trim(),
        }));
    }
    Ok(items)
}

/// Find communication logs mentioning a person.
fn find_person_comm_logs(conn: &Connection, name: &str) -> Result<Vec<Value>, String> {
    let sql = r#"
        SELECT cl.id, cl.project_id, p.name, cl.comm_date, cl.counterpart,
               cl.method, cl.summary
        FROM comm_log cl
        JOIN projects p ON p.id = cl.project_id
        WHERE cl.counterpart LIKE ?1
           OR cl.summary LIKE ?1
        ORDER BY cl.comm_date DESC
        LIMIT 50
    "#;

    let pattern = format!("%{}%", name);
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map(params![pattern], |row| {
            let id: i64 = row.get(0)?;
            let project_id: i64 = row.get(1)?;
            let proj_name: String = row.get(2)?;
            let comm_date: String = row.get(3)?;
            let counterpart: String = row.get(4)?;
            let method: String = row.get(5)?;
            let summary: String = row.get(6)?;
            Ok((
                id,
                project_id,
                proj_name,
                comm_date,
                counterpart,
                method,
                summary,
            ))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut items = Vec::new();
    for (id, project_id, proj_name, comm_date, counterpart, method, summary) in rows.flatten() {
        items.push(json!({
            "comm_id": id,
            "project_id": project_id,
            "project_name": proj_name,
            "date": comm_date,
            "counterpart": counterpart,
            "method": method,
            "summary": summary,
        }));
    }
    Ok(items)
}

/// Find action items (next_action chunks) mentioning a person.
fn find_person_actions(conn: &Connection, name: &str) -> Result<Vec<Value>, String> {
    let sql = r#"
        SELECT c.project_id, p.name, c.content
        FROM chunks c
        JOIN projects p ON p.id = c.project_id
        WHERE c.chunk_type = 'next_action'
          AND c.content LIKE ?1
        ORDER BY p.name
    "#;

    let pattern = format!("%{}%", name);
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map(params![pattern], |row| {
            let project_id: i64 = row.get(0)?;
            let proj_name: String = row.get(1)?;
            let content: String = row.get(2)?;
            Ok((project_id, proj_name, content))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut items = Vec::new();
    for (project_id, proj_name, content) in rows.flatten() {
        items.push(json!({
            "project_id": project_id,
            "project_name": proj_name,
            "action": content.trim(),
        }));
    }
    Ok(items)
}

pub struct PersonHandler;

impl super::CommandHandler for PersonHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_person(args, config)
    }

    fn compact_result(&self, data: &serde_json::Value) -> serde_json::Value {
        json!({
            "person": data.get("person"), "project_count": data.get("project_count"),
            "projects": data.get("projects").and_then(|v| v.as_array()).map(|arr| {
                arr.iter().map(|p| json!({
                    "id": p.get("id"), "name": p.get("name"), "status": p.get("status"),
                })).collect::<Vec<_>>()
            }),
        })
    }

    fn ai_hints(&self, data: &serde_json::Value) -> Vec<serde_json::Value> {
        let count = data
            .get("project_count")
            .and_then(|v| v.as_i64())
            .unwrap_or(0);
        if count >= 5 {
            vec![json!({"situation": "overloaded",
                "guide": format!("이 인물이 {}개 프로젝트를 담당합니다. 과부하 상태임을 언급하세요.", count)})]
        } else {
            vec![]
        }
    }

    fn build_bundle(
        &self,
        data: &serde_json::Value,
        conn: Option<&rusqlite::Connection>,
    ) -> serde_json::Value {
        let conn = match conn {
            Some(c) => c,
            None => return json!({}),
        };
        let mut bundle = json!({});
        if let Ok(mut stmt) = conn.prepare(
            "SELECT COUNT(*) FROM comm_log cl JOIN projects p ON p.id = cl.project_id
             WHERE p.person_internal LIKE ?1 AND cl.log_date >= date('now', '-7 days')",
        ) {
            let person = data.get("person").and_then(|v| v.as_str()).unwrap_or("");
            if !person.is_empty() {
                let pattern = format!("%{}%", crate::utils::escape_like(person));
                if let Ok(count) =
                    stmt.query_row(rusqlite::params![pattern], |r| r.get::<_, i64>(0))
                {
                    bundle["this_week_activity"] = json!({"comm_count": count});
                }
            }
        }
        bundle
    }

    fn summary(&self, data: &serde_json::Value) -> String {
        let name = data.get("person").and_then(|v| v.as_str()).unwrap_or("?");
        let count = data
            .get("project_count")
            .and_then(|v| v.as_i64())
            .unwrap_or(0);
        format!("{}: 프로젝트 {}개", name, count)
    }
}
