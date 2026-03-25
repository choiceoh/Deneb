use rusqlite::Connection;
use serde_json::{json, Value};
use regex::Regex;

use crate::config::VegaConfig;
use super::{CommandResult, open_db};

/// Urgent items query: red-status projects, stale projects, overdue actions, overloaded persons.
pub fn cmd_urgent(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("urgent", &e),
    };

    let mut urgent_items: Vec<Value> = Vec::new();

    // 1) Red status projects (critical)
    if let Ok(red) = find_red_status_projects(&conn) {
        for item in red {
            urgent_items.push(item);
        }
    }

    // 2) Overdue action items
    if let Ok(overdue) = find_overdue_actions(&conn) {
        for item in overdue {
            urgent_items.push(item);
        }
    }

    // 3) Overloaded persons (5+ active projects)
    if let Ok(overloaded) = find_overloaded_persons(&conn) {
        for item in overloaded {
            urgent_items.push(item);
        }
    }

    // 4) Stale projects (no comm_log in 30 days)
    if let Ok(stale) = find_stale_projects(&conn) {
        for item in stale {
            urgent_items.push(item);
        }
    }

    // Sort by priority: critical(0) > overdue(1) > overloaded(2) > stale(3)
    urgent_items.sort_by(|a, b| {
        let pa = a["priority"].as_i64().unwrap_or(99);
        let pb = b["priority"].as_i64().unwrap_or(99);
        pa.cmp(&pb)
    });

    let count = urgent_items.len();
    CommandResult::ok(
        "urgent",
        json!({
            "total": count,
            "items": urgent_items,
            "summary": format!("긴급 항목 {}건 발견", count),
        }),
    )
}

/// Find projects with red/critical status indicators.
fn find_red_status_projects(conn: &Connection) -> Result<Vec<Value>, String> {
    let sql = r#"
        SELECT p.id, p.name, p.status
        FROM projects p
        WHERE p.status LIKE '%🔴%'
           OR p.status LIKE '%긴급%'
           OR p.status LIKE '%중단%'
        ORDER BY p.name
    "#;

    let mut stmt = conn.prepare(sql).map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let id: i64 = row.get(0)?;
            let name: String = row.get(1)?;
            let status: String = row.get(2)?;
            Ok((id, name, status))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut items = Vec::new();
    for row in rows {
        if let Ok((id, name, status)) = row {
            items.push(json!({
                "type": "critical",
                "priority": 0,
                "project_id": id,
                "project_name": name,
                "status": status,
                "reason": format!("🔴 위험 상태: {}", status),
            }));
        }
    }
    Ok(items)
}

/// Find action items with past due dates by scanning next_action chunks.
fn find_overdue_actions(conn: &Connection) -> Result<Vec<Value>, String> {
    let sql = r#"
        SELECT c.project_id, p.name, c.content
        FROM chunks c
        JOIN projects p ON p.id = c.project_id
        WHERE c.chunk_type = 'next_action'
    "#;

    let mut stmt = conn.prepare(sql).map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let project_id: i64 = row.get(0)?;
            let name: String = row.get(1)?;
            let content: String = row.get(2)?;
            Ok((project_id, name, content))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    // Match dates like YYYY-MM-DD or YYYY.MM.DD or YYYY/MM/DD
    let date_re =
        Regex::new(r"(\d{4})[-./](\d{1,2})[-./](\d{1,2})").unwrap();
    let today = chrono_today_str();

    let mut items = Vec::new();
    for row in rows {
        if let Ok((project_id, name, content)) = row {
            for cap in date_re.captures_iter(&content) {
                let date_str = format!(
                    "{}-{:0>2}-{:0>2}",
                    &cap[1],
                    &cap[2],
                    &cap[3]
                );
                if date_str < today {
                    items.push(json!({
                        "type": "overdue",
                        "priority": 1,
                        "project_id": project_id,
                        "project_name": name,
                        "due_date": date_str,
                        "action": content.trim(),
                        "reason": format!("⏰ 기한 초과: {} ({})", content.trim(), date_str),
                    }));
                    break; // one item per action chunk
                }
            }
        }
    }
    Ok(items)
}

/// Find persons assigned to 5 or more active projects.
fn find_overloaded_persons(conn: &Connection) -> Result<Vec<Value>, String> {
    let sql = r#"
        SELECT c.content, COUNT(DISTINCT c.project_id) as cnt,
               GROUP_CONCAT(DISTINCT p.name) as projects
        FROM chunks c
        JOIN projects p ON p.id = c.project_id
        WHERE c.chunk_type = 'person'
          AND (p.status NOT LIKE '%완료%' AND p.status NOT LIKE '%중단%')
        GROUP BY c.content
        HAVING cnt >= 5
        ORDER BY cnt DESC
    "#;

    let mut stmt = conn.prepare(sql).map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let person: String = row.get(0)?;
            let cnt: i64 = row.get(1)?;
            let projects: String = row.get(2)?;
            Ok((person, cnt, projects))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut items = Vec::new();
    for row in rows {
        if let Ok((person, cnt, projects)) = row {
            items.push(json!({
                "type": "overloaded",
                "priority": 2,
                "person": person.trim(),
                "project_count": cnt,
                "projects": projects,
                "reason": format!("👤 과부하: {} ({}개 프로젝트)", person.trim(), cnt),
            }));
        }
    }
    Ok(items)
}

/// Find active projects with no communication log in the last 30 days.
fn find_stale_projects(conn: &Connection) -> Result<Vec<Value>, String> {
    let sql = r#"
        SELECT p.id, p.name, p.status,
               MAX(cl.comm_date) as last_comm
        FROM projects p
        LEFT JOIN comm_log cl ON cl.project_id = p.id
        WHERE p.status NOT LIKE '%완료%'
          AND p.status NOT LIKE '%중단%'
        GROUP BY p.id
        HAVING last_comm IS NULL
            OR last_comm < date('now', '-30 days')
        ORDER BY last_comm ASC
    "#;

    let mut stmt = conn.prepare(sql).map_err(|e| format!("쿼리 준비 실패: {}", e))?;
    let rows = stmt
        .query_map([], |row| {
            let id: i64 = row.get(0)?;
            let name: String = row.get(1)?;
            let status: String = row.get(2)?;
            let last_comm: Option<String> = row.get(3)?;
            Ok((id, name, status, last_comm))
        })
        .map_err(|e| format!("쿼리 실행 실패: {}", e))?;

    let mut items = Vec::new();
    for row in rows {
        if let Ok((id, name, _status, last_comm)) = row {
            let comm_display = last_comm
                .as_deref()
                .unwrap_or("기록 없음");
            items.push(json!({
                "type": "stale",
                "priority": 3,
                "project_id": id,
                "project_name": name,
                "last_comm": comm_display,
                "reason": format!("💤 장기 미소통: {} (마지막: {})", name, comm_display),
            }));
        }
    }
    Ok(items)
}

/// Returns today's date as YYYY-MM-DD string (no chrono dependency, uses SQLite).
fn chrono_today_str() -> String {
    // Use a simple approach: current date via std
    let now = std::time::SystemTime::now();
    let duration = now
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default();
    let secs = duration.as_secs() as i64;
    // Simple days-since-epoch calculation
    let days = secs / 86400;
    // Convert days since 1970-01-01 to YYYY-MM-DD
    let (y, m, d) = days_to_ymd(days);
    format!("{:04}-{:02}-{:02}", y, m, d)
}

fn days_to_ymd(days_since_epoch: i64) -> (i64, i64, i64) {
    // Algorithm from Howard Hinnant's date library (civil_from_days)
    let z = days_since_epoch + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = if m <= 2 { y + 1 } else { y };
    (y, m, d)
}
