use rusqlite::params;
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Recent activity query.
/// Returns the latest comm_log entries and chunks across all projects
/// within a configurable day range (default 7 days).
///
/// Args:
///   - days: number of days to look back (default 7)
///   - limit: max results per category (default 50)
///   - project: optional project filter
pub fn cmd_recent(args: &Value, config: &VegaConfig) -> CommandResult {
    let days = args.get("days").and_then(|v| v.as_i64()).unwrap_or(7);
    let limit = args.get("limit").and_then(|v| v.as_i64()).unwrap_or(50);
    let project_filter = args.get("project").and_then(|v| v.as_str());

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("recent", &e),
    };

    let since_date = chrono::Local::now()
        .checked_sub_signed(chrono::Duration::days(days))
        .map(|d| d.format("%Y-%m-%d").to_string())
        .unwrap_or_default();

    // Recent comms
    let comms = if let Some(pf) = project_filter {
        fetch_recent_comms_filtered(&conn, &since_date, limit, pf)
    } else {
        fetch_recent_comms(&conn, &since_date, limit)
    };

    // Recent chunks (by created_at or rowid)
    let chunks = if let Some(pf) = project_filter {
        fetch_recent_chunks_filtered(&conn, &since_date, limit, pf)
    } else {
        fetch_recent_chunks(&conn, &since_date, limit)
    };

    // Summary stats
    let total_comms = comms.as_ref().map(|c| c.len()).unwrap_or(0);
    let total_chunks = chunks.as_ref().map(|c| c.len()).unwrap_or(0);

    CommandResult::ok(
        "recent",
        json!({
            "since": since_date,
            "days": days,
            "comms": comms.unwrap_or_default(),
            "chunks": chunks.unwrap_or_default(),
            "total_comms": total_comms,
            "total_chunks": total_chunks,
        }),
    )
}

fn fetch_recent_comms(
    conn: &rusqlite::Connection,
    since: &str,
    limit: i64,
) -> Result<Vec<Value>, String> {
    let mut stmt = conn
        .prepare(
            "SELECT cl.id, cl.date, cl.channel, cl.sender, cl.summary,
                    p.name as project_name
             FROM comm_log cl
             LEFT JOIN projects p ON cl.project_id = p.id
             WHERE cl.date >= ?1
             ORDER BY cl.date DESC
             LIMIT ?2",
        )
        .map_err(|e| format!("커뮤니케이션 조회 실패: {e}"))?;

    let rows: Vec<Value> = stmt
        .query_map(params![since, limit], |row| {
            Ok(json!({
                "id": row.get::<_, i64>(0)?,
                "date": row.get::<_, String>(1)?,
                "channel": row.get::<_, Option<String>>(2)?,
                "sender": row.get::<_, Option<String>>(3)?,
                "summary": row.get::<_, Option<String>>(4)?,
                "project": row.get::<_, Option<String>>(5)?,
            }))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?
        .filter_map(|r| r.ok())
        .collect();

    Ok(rows)
}

fn fetch_recent_comms_filtered(
    conn: &rusqlite::Connection,
    since: &str,
    limit: i64,
    project: &str,
) -> Result<Vec<Value>, String> {
    let mut stmt = conn
        .prepare(
            "SELECT cl.id, cl.date, cl.channel, cl.sender, cl.summary,
                    p.name as project_name
             FROM comm_log cl
             LEFT JOIN projects p ON cl.project_id = p.id
             WHERE cl.date >= ?1 AND p.name = ?3
             ORDER BY cl.date DESC
             LIMIT ?2",
        )
        .map_err(|e| format!("커뮤니케이션 조회 실패: {e}"))?;

    let rows: Vec<Value> = stmt
        .query_map(params![since, limit, project], |row| {
            Ok(json!({
                "id": row.get::<_, i64>(0)?,
                "date": row.get::<_, String>(1)?,
                "channel": row.get::<_, Option<String>>(2)?,
                "sender": row.get::<_, Option<String>>(3)?,
                "summary": row.get::<_, Option<String>>(4)?,
                "project": row.get::<_, Option<String>>(5)?,
            }))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?
        .filter_map(|r| r.ok())
        .collect();

    Ok(rows)
}

fn fetch_recent_chunks(
    conn: &rusqlite::Connection,
    since: &str,
    limit: i64,
) -> Result<Vec<Value>, String> {
    let mut stmt = conn
        .prepare(
            "SELECT c.id, c.section, c.content, c.source_file,
                    p.name as project_name
             FROM chunks c
             LEFT JOIN projects p ON c.project_id = p.id
             WHERE c.created_at >= ?1
             ORDER BY c.created_at DESC
             LIMIT ?2",
        )
        .map_err(|e| format!("청크 조회 실패: {e}"))?;

    let rows: Vec<Value> = stmt
        .query_map(params![since, limit], |row| {
            Ok(json!({
                "id": row.get::<_, i64>(0)?,
                "section": row.get::<_, Option<String>>(1)?,
                "content": row.get::<_, String>(2)?,
                "source_file": row.get::<_, Option<String>>(3)?,
                "project": row.get::<_, Option<String>>(4)?,
            }))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?
        .filter_map(|r| r.ok())
        .collect();

    Ok(rows)
}

fn fetch_recent_chunks_filtered(
    conn: &rusqlite::Connection,
    since: &str,
    limit: i64,
    project: &str,
) -> Result<Vec<Value>, String> {
    let mut stmt = conn
        .prepare(
            "SELECT c.id, c.section, c.content, c.source_file,
                    p.name as project_name
             FROM chunks c
             LEFT JOIN projects p ON c.project_id = p.id
             WHERE c.created_at >= ?1 AND p.name = ?3
             ORDER BY c.created_at DESC
             LIMIT ?2",
        )
        .map_err(|e| format!("청크 조회 실패: {e}"))?;

    let rows: Vec<Value> = stmt
        .query_map(params![since, limit, project], |row| {
            Ok(json!({
                "id": row.get::<_, i64>(0)?,
                "section": row.get::<_, Option<String>>(1)?,
                "content": row.get::<_, String>(2)?,
                "source_file": row.get::<_, Option<String>>(3)?,
                "project": row.get::<_, Option<String>>(4)?,
            }))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?
        .filter_map(|r| r.ok())
        .collect();

    Ok(rows)
}
