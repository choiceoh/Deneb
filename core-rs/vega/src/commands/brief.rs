//! Structured project brief command.
//!
//! Port of Python vega/commands/brief.py — full structured brief with
//! next_actions, risks, key_points, recent_comms, recommended_commands.
//! Supports multi-project brief ("brief 5 10 15").

use regex::Regex;
use rusqlite::{params, Connection};
use serde_json::{json, Value};
use std::sync::LazyLock;

use crate::config::VegaConfig;
use crate::utils::extract_bullets;

#[allow(clippy::expect_used)]
static RISK_KEYWORD_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(이슈|리스크|지연|미정|보류|주의|대응|중단|긴급)").expect("valid regex")
});

use super::{find_project_id, open_db, CommandResult};

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
    type ChunkBucket<'a> =
        std::collections::HashMap<String, Vec<&'a (String, String, String, Option<String>)>>;
    let mut bucket: ChunkBucket = std::collections::HashMap::new();
    for chunk in &chunks {
        bucket.entry(chunk.2.clone()).or_default().push(chunk);
    }

    // 3. Next actions from next_action chunks
    let mut next_actions: Vec<String> = Vec::new();
    for ch in bucket
        .get("next_action")
        .unwrap_or(&Vec::new())
        .iter()
        .take(3)
    {
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
        for ch in bucket.get("status").unwrap_or(&Vec::new()).iter().take(2) {
            for bullet in extract_bullets(&ch.1, 4) {
                if RISK_KEYWORD_RE.is_match(&bullet) {
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
#[allow(clippy::expect_used)]
mod tests {
    use super::*;
    use crate::db::schema::init_db;

    fn setup_test_db() -> Result<Connection, Box<dyn std::error::Error>> {
        let conn = Connection::open_in_memory()?;
        init_db(&conn)?;

        conn.execute(
            "INSERT INTO projects (id, name, client, status, person_internal)
             VALUES (1, '비금도 태양광', '한국전력', '진행중', '김대희')",
            [],
        )?;

        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '다음 예상 액션', '- 인허가 서류 제출\n- 현장 답사 예정', 'next_action')",
            [],
        )?;

        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '이슈', '- 지연 가능성 있음\n- 자재 수급 불안정', 'issue')",
            [],
        )?;

        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '현재 상태', '전반적으로 순조로운 진행 중', 'status')",
            [],
        )?;

        conn.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
             VALUES (1, '2026-03-25', '김대희', '진행상황 보고', '인허가 진행 중')",
            [],
        )?;

        Ok(conn)
    }

    #[test]
    fn test_build_single_brief() -> Result<(), Box<dyn std::error::Error>> {
        let conn = setup_test_db()?;
        let brief = build_single_brief(&conn, 1)?;

        assert_eq!(brief.get("project_id").expect("project_id key"), 1);
        assert_eq!(
            brief
                .get("project_name")
                .expect("project_name key")
                .as_str()
                .expect("project_name as str"),
            "비금도 태양광"
        );

        let actions = brief
            .get("next_actions")
            .expect("next_actions key")
            .as_array()
            .expect("next_actions as array");
        assert!(!actions.is_empty(), "next_actions should not be empty");

        let risks = brief
            .get("risks")
            .expect("risks key")
            .as_array()
            .expect("risks as array");
        assert!(!risks.is_empty(), "risks should not be empty");

        let comms = brief
            .get("recent_comms")
            .expect("recent_comms key")
            .as_array()
            .expect("recent_comms as array");
        assert_eq!(comms.len(), 1);

        let cmds = brief
            .get("recommended_commands")
            .expect("recommended_commands key")
            .as_array()
            .expect("recommended_commands as array");
        assert!(cmds.len() >= 3);

        Ok(())
    }

    #[test]
    fn test_parse_multi_ids() {
        assert_eq!(parse_multi_ids("5 10 15"), vec![5, 10, 15]);
        assert_eq!(parse_multi_ids("hello"), Vec::<i64>::new());
        assert_eq!(parse_multi_ids("3"), vec![3]);
    }

    #[test]
    fn test_brief_not_found() -> Result<(), Box<dyn std::error::Error>> {
        let conn = Connection::open_in_memory()?;
        init_db(&conn)?;
        let result = build_single_brief(&conn, 999);
        assert!(result.is_err());
        Ok(())
    }
}

pub struct BriefHandler;

impl super::CommandHandler for BriefHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_brief(args, config)
    }

    fn compact_result(&self, data: &serde_json::Value) -> serde_json::Value {
        let mut kept = json!({});
        for key in [
            "project_id",
            "project_name",
            "status",
            "client",
            "person_internal",
            "latest_activity",
            "next_actions",
            "risks",
        ] {
            if let Some(v) = data.get(key) {
                kept[key] = v.clone();
            }
        }
        kept["comm_count"] = json!(data
            .get("recent_comms")
            .and_then(|v| v.as_array())
            .map(|a| a.len())
            .unwrap_or(0));
        kept
    }

    fn ai_hints(&self, data: &serde_json::Value) -> Vec<serde_json::Value> {
        let mut hints: Vec<serde_json::Value> = Vec::new();
        if data
            .get("risks")
            .and_then(|v| v.as_array())
            .map(|a| !a.is_empty())
            .unwrap_or(false)
        {
            hints.push(json!({"situation": "has_risks",
                "guide": "리스크 항목이 있습니다. 상태/액션 보고 후 리스크를 별도 강조하세요."}));
        }
        if data
            .get("next_actions")
            .and_then(|v| v.as_array())
            .map(|a| a.is_empty())
            .unwrap_or(true)
        {
            let pid = data.get("project_id").and_then(|v| v.as_i64()).unwrap_or(0);
            hints.push(json!({"situation": "no_actions",
                "guide": "다음 액션이 비어 있습니다. 액션 추가를 물어보세요.",
                "suggested_followup": format!("add-action {}", pid)}));
        }
        hints
    }

    fn build_bundle(
        &self,
        data: &serde_json::Value,
        conn: Option<&Connection>,
    ) -> serde_json::Value {
        let conn = match conn {
            Some(c) => c,
            None => return json!({}),
        };
        let mut bundle = json!({});
        let pid = data.get("project_id").and_then(|v| v.as_i64());
        if let Some(pid) = pid {
            // Check overdue actions
            if let Ok(mut stmt) = conn.prepare(
                "SELECT content FROM chunks WHERE project_id=?1 AND chunk_type='next_action'",
            ) {
                let today = chrono::Local::now().format("%Y-%m-%d").to_string();
                if let Ok(rows) = stmt.query_map(rusqlite::params![pid], |r| r.get::<_, String>(0))
                {
                    if let Ok(date_re) = regex::Regex::new(r"20\d{2}[-/]\d{2}[-/]\d{2}") {
                        for content in rows.flatten() {
                            for m in date_re.find_iter(&content) {
                                let d = m.as_str().replace('/', "-");
                                if d <= today {
                                    bundle["urgency"] = json!({"priority": "overdue",
                                        "reason": format!("기한 도래: {}", d)});
                                    break;
                                }
                            }
                        }
                    }
                }
            }
            // Related projects by same person
            let person = data
                .get("person_internal")
                .and_then(|v| v.as_str())
                .unwrap_or("");
            if let Some(first_name) = person.split_whitespace().next() {
                if !first_name.is_empty() {
                    if let Ok(mut stmt) = conn.prepare(
                        "SELECT id, name, status FROM projects WHERE person_internal LIKE ?1 AND id != ?2 LIMIT 3"
                    ) {
                        let pattern = format!("%{}%", crate::utils::escape_like(first_name));
                        if let Ok(rows) = stmt.query_map(rusqlite::params![pattern, pid], |r| {
                            Ok(json!({"id": r.get::<_, i64>(0)?, "name": r.get::<_, String>(1)?, "status": r.get::<_, String>(2)?}))
                        }) {
                            let related: Vec<serde_json::Value> = rows.flatten().collect();
                            if !related.is_empty() {
                                bundle["related_projects"] = json!(related);
                            }
                        }
                    }
                }
            }
        }
        bundle
    }

    fn summary(&self, data: &serde_json::Value) -> String {
        if data.get("briefs").is_some() {
            let count = data.get("count").and_then(|v| v.as_i64()).unwrap_or(0);
            format!("{}개 프로젝트 브리프", count)
        } else {
            let name = data
                .get("project_name")
                .and_then(|v| v.as_str())
                .unwrap_or("?");
            let actions = data
                .get("next_actions")
                .and_then(|v| v.as_array())
                .map(|a| a.len())
                .unwrap_or(0);
            let risks = data
                .get("risks")
                .and_then(|v| v.as_array())
                .map(|a| a.len())
                .unwrap_or(0);
            format!(
                "[{}] {} — 액션 {}개, 리스크 {}개",
                data.get("project_id").and_then(|v| v.as_i64()).unwrap_or(0),
                name,
                actions,
                risks
            )
        }
    }
}
