use rusqlite::Connection;
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Dashboard overview: status distribution, person workload, recent activity,
/// and key metrics across all projects.
pub fn cmd_dashboard(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("dashboard", &e),
    };

    let status_groups = match get_status_groups(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("dashboard", &e),
    };
    let person_workload = match get_person_workload(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("dashboard", &e),
    };
    let recent_activity = match get_recent_activity(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("dashboard", &e),
    };
    let key_metrics = match get_key_metrics(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("dashboard", &e),
    };

    CommandResult::ok(
        "dashboard",
        json!({
            "status_groups": status_groups,
            "person_workload": person_workload,
            "recent_activity": recent_activity,
            "metrics": key_metrics
        }),
    )
}

/// Group projects by status category.
fn get_status_groups(conn: &Connection) -> Result<Value, String> {
    let sql = "
        SELECT
            CASE
                WHEN status LIKE '%🔴%' OR status LIKE '%긴급%' OR status LIKE '%중단%' THEN 'critical'
                WHEN status LIKE '%🟡%' OR status LIKE '%주의%' THEN 'warning'
                WHEN status LIKE '%🟢%' OR status LIKE '%정상%' OR status LIKE '%진행%' THEN 'active'
                WHEN status LIKE '%완료%' OR status LIKE '%종료%' THEN 'completed'
                WHEN status LIKE '%대기%' OR status LIKE '%보류%' THEN 'pending'
                ELSE 'other'
            END AS status_group,
            COUNT(*) AS count,
            GROUP_CONCAT(title, ', ') AS projects
        FROM projects
        GROUP BY status_group
        ORDER BY
            CASE status_group
                WHEN 'critical' THEN 1
                WHEN 'warning' THEN 2
                WHEN 'active' THEN 3
                WHEN 'pending' THEN 4
                WHEN 'completed' THEN 5
                ELSE 6
            END
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let group: String = row.get(0)?;
            let count: i64 = row.get(1)?;
            let projects: Option<String> = row.get(2)?;
            Ok(json!({
                "group": group,
                "count": count,
                "projects": projects.unwrap_or_default()
                    .split(", ")
                    .filter(|s| !s.is_empty())
                    .collect::<Vec<&str>>()
            }))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut groups = Vec::new();
    for row in rows {
        groups.push(row.map_err(|e| format!("행 읽기 실패: {}", e))?);
    }
    Ok(json!(groups))
}

/// Calculate workload per person across active projects.
fn get_person_workload(conn: &Connection) -> Result<Value, String> {
    let sql = "
        SELECT c.body, COUNT(DISTINCT c.project_id) AS project_count,
               GROUP_CONCAT(DISTINCT p.title) AS projects
        FROM chunks c
        JOIN projects p ON c.project_id = p.id
        WHERE c.chunk_type IN ('person_internal', 'person_external')
          AND p.status NOT LIKE '%완료%'
          AND p.status NOT LIKE '%종료%'
        GROUP BY c.body
        HAVING project_count >= 1
        ORDER BY project_count DESC
        LIMIT 20
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let body: String = row.get(0)?;
            let count: i64 = row.get(1)?;
            let projects: Option<String> = row.get(2)?;
            let name = body.lines().next().unwrap_or(&body).trim().to_string();
            let status = if count >= 5 {
                "overloaded"
            } else if count >= 3 {
                "heavy"
            } else {
                "normal"
            };
            Ok(json!({
                "name": name,
                "project_count": count,
                "status": status,
                "projects": projects.unwrap_or_default()
                    .split(',')
                    .map(|s| s.trim())
                    .filter(|s| !s.is_empty())
                    .collect::<Vec<&str>>()
            }))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut workload = Vec::new();
    for row in rows {
        workload.push(row.map_err(|e| format!("행 읽기 실패: {}", e))?);
    }
    Ok(json!(workload))
}

/// Get recent communication activity across projects.
fn get_recent_activity(conn: &Connection) -> Result<Value, String> {
    let sql = "
        SELECT cl.logged_at, cl.summary, p.title, p.id
        FROM comm_log cl
        JOIN projects p ON cl.project_id = p.id
        ORDER BY cl.logged_at DESC
        LIMIT 15
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let logged_at: String = row.get(0)?;
            let summary: String = row.get(1)?;
            let title: String = row.get(2)?;
            let project_id: i64 = row.get(3)?;
            Ok(json!({
                "date": logged_at,
                "summary": summary,
                "project": title,
                "project_id": project_id
            }))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut activity = Vec::new();
    for row in rows {
        activity.push(row.map_err(|e| format!("행 읽기 실패: {}", e))?);
    }
    Ok(json!(activity))
}

/// Calculate key metrics for the dashboard.
fn get_key_metrics(conn: &Connection) -> Result<Value, String> {
    let total_projects: i64 = conn
        .query_row("SELECT COUNT(*) FROM projects", [], |row| row.get(0))
        .map_err(|e| format!("쿼리 실패: {}", e))?;

    let active_projects: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM projects WHERE status NOT LIKE '%완료%' AND status NOT LIKE '%종료%'",
            [],
            |row| row.get(0),
        )
        .map_err(|e| format!("쿼리 실패: {}", e))?;

    let total_comm_logs: i64 = conn
        .query_row("SELECT COUNT(*) FROM comm_log", [], |row| row.get(0))
        .map_err(|e| format!("쿼리 실패: {}", e))?;

    let recent_comm_count: i64 = conn
        .query_row(
            "SELECT COUNT(*) FROM comm_log WHERE logged_at >= date('now', '-7 days')",
            [],
            |row| row.get(0),
        )
        .map_err(|e| format!("쿼리 실패: {}", e))?;

    let total_chunks: i64 = conn
        .query_row("SELECT COUNT(*) FROM chunks", [], |row| row.get(0))
        .map_err(|e| format!("쿼리 실패: {}", e))?;

    let total_tags: i64 = conn
        .query_row("SELECT COUNT(*) FROM tags", [], |row| row.get(0))
        .map_err(|e| format!("쿼리 실패: {}", e))?;

    let stale_count: i64 = conn
        .query_row(
            "SELECT COUNT(DISTINCT p.id) FROM projects p
             LEFT JOIN comm_log cl ON cl.project_id = p.id
             WHERE p.status NOT LIKE '%완료%'
               AND p.status NOT LIKE '%종료%'
             GROUP BY p.id
             HAVING MAX(cl.logged_at) < date('now', '-30 days')
                OR MAX(cl.logged_at) IS NULL",
            [],
            |row| row.get(0),
        )
        .unwrap_or(0);

    Ok(json!({
        "total_projects": total_projects,
        "active_projects": active_projects,
        "completed_projects": total_projects - active_projects,
        "total_comm_logs": total_comm_logs,
        "recent_comm_logs_7d": recent_comm_count,
        "total_chunks": total_chunks,
        "total_tags": total_tags,
        "stale_projects": stale_count
    }))
}

pub struct DashboardHandler;

impl super::CommandHandler for DashboardHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_dashboard(args, config)
    }

    fn compact_result(&self, data: &serde_json::Value) -> serde_json::Value {
        json!({
            "total_projects": data.get("total_projects"),
            "active_projects": data.get("active_projects"),
            "overloaded_persons": data.get("overloaded_persons"),
        })
    }

    fn ai_hints(&self, data: &serde_json::Value) -> Vec<serde_json::Value> {
        let _ = data;
        vec![json!({"situation": "dashboard_overview",
            "guide": "전체 현황입니다. 긴급: urgent, 금액: pipeline으로 확인하세요."})]
    }

    fn summary(&self, data: &serde_json::Value) -> String {
        let total = data
            .get("total_projects")
            .and_then(|v| v.as_i64())
            .unwrap_or(0);
        let active = data
            .get("active_projects")
            .and_then(|v| v.as_i64())
            .unwrap_or(0);
        format!("전체 {}개 중 활성 {}개", total, active)
    }
}
