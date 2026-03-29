use chrono::Datelike;
use rusqlite::{params, Connection};
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Weekly report: for each project, gather recent comms and chunks since a given date.
/// Defaults to this Monday if no `since` argument is provided.
pub fn cmd_weekly(args: &Value, config: &VegaConfig) -> CommandResult {
    let since = resolve_since(args);

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("weekly", &e),
    };

    let projects = match fetch_active_projects(&conn, &since) {
        Ok(p) => p,
        Err(e) => return CommandResult::err("weekly", &e),
    };

    let total_activity: i64 = projects
        .iter()
        .map(|p| {
            p.get("comm_count").and_then(|v| v.as_i64()).unwrap_or(0)
                + p.get("chunk_count").and_then(|v| v.as_i64()).unwrap_or(0)
        })
        .sum();

    let active_count = projects.len();

    CommandResult::ok(
        "weekly",
        json!({
            "period": {
                "since": since,
                "until": today_str(),
            },
            "active_projects": active_count,
            "total_activity": total_activity,
            "projects": projects,
        }),
    )
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Determine the `since` date string (YYYY-MM-DD).
/// Falls back to this Monday if the arg is missing or unparseable.
fn resolve_since(args: &Value) -> String {
    if let Some(s) = args.get("since").and_then(|v| v.as_str()) {
        // Validate the format loosely
        if s.len() == 10 && s.chars().filter(|c| *c == '-').count() == 2 {
            return s.to_string();
        }
    }
    this_monday()
}

/// Return the ISO date string for today.
fn today_str() -> String {
    let now = chrono::Local::now().date_naive();
    now.format("%Y-%m-%d").to_string()
}

/// Return the ISO date string for the Monday of the current week.
fn this_monday() -> String {
    let now = chrono::Local::now().date_naive();
    let weekday = now.weekday().num_days_from_monday(); // Mon=0
    let monday = now - chrono::Duration::days(weekday as i64);
    monday.format("%Y-%m-%d").to_string()
}

/// Fetch projects that have activity (comms or chunks) since the given date.
fn fetch_active_projects(conn: &Connection, since: &str) -> Result<Vec<Value>, String> {
    let mut stmt = conn
        .prepare("SELECT id, name, client, status FROM projects ORDER BY name")
        .map_err(|e| format!("프로젝트 조회 실패: {e}"))?;

    let rows: Vec<(i64, String, Option<String>, Option<String>)> = stmt
        .query_map([], |row| {
            Ok((row.get(0)?, row.get(1)?, row.get(2)?, row.get(3)?))
        })
        .map_err(|e| format!("프로젝트 조회 실패: {e}"))?
        .filter_map(|r| r.ok())
        .collect();

    let mut projects: Vec<Value> = Vec::new();

    for (pid, name, client, status) in rows {
        let comms = fetch_recent_comms(conn, pid, since)?;
        let chunks = fetch_recent_chunks(conn, pid, since)?;

        if comms.is_empty() && chunks.is_empty() {
            continue;
        }

        projects.push(json!({
            "id": pid,
            "name": name,
            "client": client,
            "status": status,
            "comm_count": comms.len(),
            "chunk_count": chunks.len(),
            "comms": comms,
            "chunks": chunks,
        }));
    }

    Ok(projects)
}

/// Fetch comm_log entries for a project since a date.
fn fetch_recent_comms(
    conn: &Connection,
    project_id: i64,
    since: &str,
) -> Result<Vec<Value>, String> {
    let mut stmt = conn
        .prepare(
            "SELECT date, channel, summary FROM comm_log
             WHERE project_id = ?1 AND date >= ?2
             ORDER BY date DESC",
        )
        .map_err(|e| format!("커뮤니케이션 조회 실패: {e}"))?;

    let rows = stmt
        .query_map(params![project_id, since], |row| {
            let date: String = row.get(0)?;
            let channel: Option<String> = row.get(1)?;
            let summary: Option<String> = row.get(2)?;
            Ok(json!({
                "date": date,
                "channel": channel,
                "summary": summary,
            }))
        })
        .map_err(|e| format!("커뮤니케이션 조회 실패: {e}"))?
        .filter_map(|r| r.ok())
        .collect();

    Ok(rows)
}

/// Fetch chunks modified/created for a project since a date.
fn fetch_recent_chunks(
    conn: &Connection,
    project_id: i64,
    since: &str,
) -> Result<Vec<Value>, String> {
    let mut stmt = conn
        .prepare(
            "SELECT heading, updated_at FROM chunks
             WHERE project_id = ?1 AND updated_at >= ?2
             ORDER BY updated_at DESC",
        )
        .map_err(|e| format!("청크 조회 실패: {e}"))?;

    let rows = stmt
        .query_map(params![project_id, since], |row| {
            let heading: Option<String> = row.get(0)?;
            let updated_at: String = row.get(1)?;
            Ok(json!({
                "heading": heading,
                "updated_at": updated_at,
            }))
        })
        .map_err(|e| format!("청크 조회 실패: {e}"))?
        .filter_map(|r| r.ok())
        .collect();

    Ok(rows)
}

pub struct WeeklyHandler;

impl super::CommandHandler for WeeklyHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_weekly(args, config)
    }

    fn summary(&self, data: &serde_json::Value) -> String {
        let active = data
            .get("active_projects")
            .and_then(|v| v.as_i64())
            .unwrap_or(0);
        format!("활성 프로젝트 {}개", active)
    }
}
