//! Structured project brief command.
//!
//! Port of Python vega/commands/brief.py — full structured brief with
//! next_actions, risks, key_points, recent_comms, recommended_commands.
//! Supports multi-project brief ("brief 5 10 15").

use regex::Regex;
use rusqlite::{params, Connection};
use serde_json::{json, Value};

use crate::config::VegaConfig;
use crate::utils::extract_bullets;

use super::{open_db, find_project_id, CommandResult};

/// Build a structured brief for one project by ID.
/// Public so other commands (search, ask) can call it for auto-brief.
pub fn build_single_brief(conn: &Connection, pid: i64) -> Result<Value, String> {
    // 1. Project metadata
    let proj = conn
        .query_row(
            "SELECT id, name, client, status, capacity, biz_type,
                    person_internal, person_external, partner
             FROM projects WHERE id=?1",
            params![pid],
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
        )
        .map_err(|_| format!("프로젝트 ID {} 없음", pid))?;

    let proj_name = proj
        .get("name")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();
    let status = proj
        .get("status")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .to_string();

    // 2. Chunks by type
    let mut stmt = conn
        .prepare(
            "SELECT section_heading, content, chunk_type, entry_date
             FROM chunks WHERE project_id=?1
             ORDER BY COALESCE(entry_date, '0000-00-00') DESC, id DESC",
        )
        .map_err(|e| e.to_string())?;

    let chunks: Vec<(String, String, String, Option<String>)> = stmt
        .query_map(params![pid], |r| {
            Ok((
                r.get::<_, Option<String>>(0)?.unwrap_or_default(),
                r.get::<_, Option<String>>(1)?.unwrap_or_default(),
                r.get::<_, Option<String>>(2)?.unwrap_or_default(),
                r.get::<_, Option<String>>(3)?,
            ))
        })
        .map_err(|e| e.to_string())?
        .filter_map(|r| r.ok())
        .collect();

    // Bucket by chunk_type
    let mut bucket: std::collections::HashMap<String, Vec<&(String, String, String, Option<String>)>> =
        std::collections::HashMap::new();
    for chunk in &chunks {
        bucket.entry(chunk.2.clone()).or_default().push(chunk);
    }

    // 3. Next actions from next_action chunks
    let mut next_actions: Vec<String> = Vec::new();
    for ch in bucket.get("next_action").unwrap_or(&Vec::new()).iter().take(3) {
        next_actions.extend(extract_bullets(&ch.1, 3));
    }
    next_actions.truncate(5);

    // 4. Risks from issue chunks + status risk keywords
    let mut risks: Vec<String> = Vec::new();
    for ch in bucket.get("issue").unwrap_or(&Vec::new()).iter().take(3) {
        risks.extend(extract_bullets(&ch.1, 3));
    }
    if risks.is_empty() {
        // Scan status chunks for risk keywords
        let risk_re = Regex::new(r"(이슈|리스크|지연|미정|보류|주의|대응|중단|긴급)").unwrap();
        for ch in bucket.get("status").unwrap_or(&Vec::new()).iter().take(2) {
            for bullet in extract_bullets(&ch.1, 4) {
                if risk_re.is_match(&bullet) {
                    risks.push(bullet);
                }
            }
        }
    }
    risks.truncate(4);

    // 5. Key points from summary/status/history/technical
    let mut key_points: Vec<String> = Vec::new();
    for ctype in &["summary", "status", "history", "technical"] {
        for ch in bucket.get(*ctype).unwrap_or(&Vec::new()).iter().take(2) {
            key_points.extend(extract_bullets(&ch.1, 2));
        }
        if key_points.len() >= 6 {
            break;
        }
    }
    // Deduplicate
    let mut deduped: Vec<String> = Vec::new();
    for point in key_points {
        if !deduped.contains(&point) {
            deduped.push(point);
        }
    }
    let key_points = deduped.into_iter().take(6).collect::<Vec<_>>();

    // 6. Recent communications
    let mut comm_stmt = conn
        .prepare(
            "SELECT log_date, sender, subject, summary
             FROM comm_log WHERE project_id=?1
             ORDER BY log_date DESC, id DESC LIMIT 5",
        )
        .map_err(|e| e.to_string())?;

    let comms: Vec<Value> = comm_stmt
        .query_map(params![pid], |r| {
            Ok(json!({
                "date": r.get::<_, Option<String>>(0)?,
                "sender": r.get::<_, Option<String>>(1)?,
                "subject": r.get::<_, Option<String>>(2)?,
                "summary": r.get::<_, Option<String>>(3)?
                    .map(|s| s.chars().take(160).collect::<String>()),
            }))
        })
        .map_err(|e| e.to_string())?
        .filter_map(|r| r.ok())
        .collect();

    let recent_comms: Vec<&Value> = comms.iter().take(3).collect();

    // 7. Latest activity date
    let mut all_dates: Vec<String> = Vec::new();
    for ch in &chunks {
        if let Some(ref d) = ch.3 {
            all_dates.push(d.clone());
        }
    }
    for c in &comms {
        if let Some(d) = c.get("date").and_then(|v| v.as_str()) {
            all_dates.push(d.to_string());
        }
    }
    let latest_activity = all_dates.iter().max().cloned();

    // 8. Recommended commands
    let recommended_commands = vec![
        format!("show {}", pid),
        format!("timeline {}", pid),
        format!("search {}", proj_name),
    ];

    Ok(json!({
        "project_id": pid,
        "project_name": proj_name,
        "client": proj.get("client"),
        "status": status,
        "capacity": proj.get("capacity"),
        "biz_type": proj.get("biz_type"),
        "person_internal": proj.get("person_internal"),
        "person_external": proj.get("person_external"),
        "partner": proj.get("partner"),
        "latest_activity": latest_activity,
        "next_actions": next_actions,
        "risks": risks,
        "key_points": key_points,
        "recent_comms": recent_comms,
        "recommended_commands": recommended_commands,
    }))
}

/// Parse multiple project IDs from space-separated text.
fn parse_multi_ids(text: &str) -> Vec<i64> {
    text.split_whitespace()
        .filter_map(|s| s.parse::<i64>().ok())
        .collect()
}

/// Brief command handler.
pub fn cmd_brief(args: &Value, config: &VegaConfig) -> CommandResult {
    let query = args
        .get("query")
        .and_then(|v| v.as_str())
        .unwrap_or("")
        .trim();

    if query.is_empty() {
        return CommandResult::err("brief", "프로젝트 ID 또는 이름이 필요합니다");
    }

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("brief", &e),
    };

    // Multi-project brief: "brief 5 10 15"
    let multi_ids = parse_multi_ids(query);
    if multi_ids.len() >= 2 {
        let mut briefs: Vec<Value> = Vec::new();
        for id in &multi_ids {
            match build_single_brief(&conn, *id) {
                Ok(b) => briefs.push(b),
                Err(e) => briefs.push(json!({"error": e})),
            }
        }
        return CommandResult::ok(
            "brief",
            json!({
                "briefs": briefs,
                "count": briefs.len(),
                "summary": format!("{}개 프로젝트 브리프", briefs.len()),
            }),
        );
    }

    // Single project: try ID, LIKE, then fuzzy
    let pid = find_project_id(config, query);
    match pid {
        Some(id) => match build_single_brief(&conn, id) {
            Ok(data) => CommandResult::ok("brief", data),
            Err(e) => CommandResult::err("brief", &e),
        },
        None => CommandResult::err(
            "brief",
            "프로젝트를 특정할 수 없습니다. ID 또는 프로젝트명을 포함해주세요.",
        ),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::db::schema::init_db;

    fn setup_test_db() -> Connection {
        let conn = Connection::open_in_memory().unwrap();
        init_db(&conn).unwrap();

        conn.execute(
            "INSERT INTO projects (id, name, client, status, person_internal)
             VALUES (1, '비금도 태양광', '한국전력', '진행중', '김대희')",
            [],
        )
        .unwrap();

        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '다음 예상 액션', '- 인허가 서류 제출\n- 현장 답사 예정', 'next_action')",
            [],
        )
        .unwrap();

        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '이슈', '- 지연 가능성 있음\n- 자재 수급 불안정', 'issue')",
            [],
        )
        .unwrap();

        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '현재 상태', '전반적으로 순조로운 진행 중', 'status')",
            [],
        )
        .unwrap();

        conn.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
             VALUES (1, '2026-03-25', '김대희', '진행상황 보고', '인허가 진행 중')",
            [],
        )
        .unwrap();

        conn
    }

    #[test]
    fn test_build_single_brief() {
        let conn = setup_test_db();
        let brief = build_single_brief(&conn, 1).unwrap();

        assert_eq!(brief.get("project_id").unwrap(), 1);
        assert_eq!(
            brief.get("project_name").unwrap().as_str().unwrap(),
            "비금도 태양광"
        );

        let actions = brief.get("next_actions").unwrap().as_array().unwrap();
        assert!(!actions.is_empty(), "next_actions should not be empty");

        let risks = brief.get("risks").unwrap().as_array().unwrap();
        assert!(!risks.is_empty(), "risks should not be empty");

        let comms = brief.get("recent_comms").unwrap().as_array().unwrap();
        assert_eq!(comms.len(), 1);

        let cmds = brief.get("recommended_commands").unwrap().as_array().unwrap();
        assert!(cmds.len() >= 3);
    }

    #[test]
    fn test_parse_multi_ids() {
        assert_eq!(parse_multi_ids("5 10 15"), vec![5, 10, 15]);
        assert_eq!(parse_multi_ids("hello"), Vec::<i64>::new());
        assert_eq!(parse_multi_ids("3"), vec![3]);
    }

    #[test]
    fn test_brief_not_found() {
        let conn = Connection::open_in_memory().unwrap();
        init_db(&conn).unwrap();
        let result = build_single_brief(&conn, 999);
        assert!(result.is_err());
    }
}
