//! AI helpers for Vega — LLM context injection, depth filtering, auto-correction.
//!
//! Port of Python vega/core.py AI functions (E-2 through E-7).

use regex::Regex;
use rusqlite::Connection;
use serde_json::{json, Value};

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// E-2: Depth filtering
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Apply depth filter to command results.
/// "compact" = minimal, "brief" = summary, "normal" = default, "detailed"/"full" = everything.
pub fn apply_depth(data: &Value, command: &str, depth: &str) -> Value {
    match depth {
        "compact" | "brief" => compact_result(data, command),
        "detailed" | "full" => data.clone(),
        _ => data.clone(),
    }
}

fn compact_result(data: &Value, command: &str) -> Value {
    match command {
        "search" => {
            let projects = data
                .get("projects")
                .and_then(|v| v.as_array())
                .map(|arr| {
                    arr.iter()
                        .take(5)
                        .map(|p| {
                            json!({
                                "id": p.get("id"), "name": p.get("name"),
                                "status": p.get("status"), "score": p.get("score"),
                            })
                        })
                        .collect::<Vec<_>>()
                })
                .unwrap_or_default();
            json!({
                "projects": projects,
                "result_count": data.get("result_count"),
                "matched_keywords": data.get("matched_keywords"),
                "follow_up_hint": data.get("follow_up_hint"),
            })
        }
        "urgent" => json!({
            "total": data.get("total"), "critical": data.get("critical"),
            "overdue": data.get("overdue"), "stale": data.get("stale"),
            "items": data.get("items").and_then(|v| v.as_array()).map(|arr| {
                arr.iter().take(5).map(|i| json!({
                    "project_name": i.get("project_name"),
                    "priority": i.get("priority"), "reason": i.get("reason"),
                })).collect::<Vec<_>>()
            }),
        }),
        "show" => json!({
            "id": data.get("id").or(data.get("project").and_then(|p| p.get("id"))),
            "name": data.get("name").or(data.get("project").and_then(|p| p.get("name"))),
            "status": data.get("status").or(data.get("project").and_then(|p| p.get("status"))),
            "client": data.get("client").or(data.get("project").and_then(|p| p.get("client"))),
            "person_internal": data.get("person_internal").or(data.get("project").and_then(|p| p.get("person_internal"))),
            "section_count": data.get("sections").and_then(|v| v.as_array()).map(|a| a.len()),
            "comm_count": data.get("communications").and_then(|v| v.as_array()).map(|a| a.len()),
        }),
        "brief" => {
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
        "dashboard" => json!({
            "total_projects": data.get("total_projects"),
            "active_projects": data.get("active_projects"),
            "overloaded_persons": data.get("overloaded_persons"),
        }),
        "person" => json!({
            "person": data.get("person"), "project_count": data.get("project_count"),
            "projects": data.get("projects").and_then(|v| v.as_array()).map(|arr| {
                arr.iter().map(|p| json!({
                    "id": p.get("id"), "name": p.get("name"), "status": p.get("status"),
                })).collect::<Vec<_>>()
            }),
        }),
        "list" => json!({
            "total": data.get("projects").and_then(|v| v.as_array()).map(|a| a.len()),
            "projects": data.get("projects").and_then(|v| v.as_array()).map(|arr| {
                arr.iter().map(|p| json!({
                    "id": p.get("id"), "name": p.get("name"), "status": p.get("status"),
                })).collect::<Vec<_>>()
            }),
        }),
        "compare" => json!({
            "project_count": data.get("project_count"),
            "projects": data.get("projects").and_then(|v| v.as_array()).map(|arr| {
                arr.iter().map(|p| json!({
                    "id": p.get("id"), "name": p.get("name"),
                    "status": p.get("status"), "client": p.get("client"),
                })).collect::<Vec<_>>()
            }),
            "shared": data.get("shared"), "summary": data.get("summary"),
        }),
        "stats" => json!({
            "projects": data.get("projects"),
            "communication": { "total": data.get("communication").and_then(|c| c.get("total")) },
            "summary": data.get("summary"),
        }),
        _ => data.clone(),
    }
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// E-3: AI behavioral hints
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Build situational AI hints for LLM context injection.
/// Port of Python _build_ai_hint with 15 situational patterns.
pub fn build_ai_hint(command: &str, data: &Value) -> Value {
    let result_type = match command {
        "search" => "검색 결과",
        "show" => "프로젝트 상세",
        "brief" => "브리프",
        "urgent" => "긴급 항목",
        "dashboard" => "대시보드",
        "pipeline" => "파이프라인",
        "weekly" => "주간 보고",
        "contacts" => "연락처",
        "cross" => "크로스 분석",
        "person" => "인물 포트폴리오",
        "compare" => "프로젝트 비교",
        "timeline" => "타임라인",
        "list" => "프로젝트 목록",
        _ => "기타",
    };

    let mut hints: Vec<Value> = Vec::new();

    match command {
        "search" => {
            let n = data
                .get("result_count")
                .and_then(|rc| rc.get("projects"))
                .and_then(|v| v.as_i64())
                .unwrap_or(0);
            if n == 0 {
                hints.push(json!({"situation": "no_results",
                    "guide": "검색 결과가 없습니다. suggestions 필드의 후보를 안내하거나 키워드 변경을 제안하세요."}));
            } else if n == 1 {
                let pid = data
                    .get("projects")
                    .and_then(|v| v.as_array())
                    .and_then(|a| a.first())
                    .and_then(|p| p.get("id"))
                    .and_then(|v| v.as_i64())
                    .unwrap_or(0);
                hints.push(json!({"situation": "single_match",
                    "guide": format!("정확히 1개 프로젝트 매칭. 추가 정보: brief {}", pid)}));
            } else if n > 5 {
                hints.push(json!({"situation": "too_many_results",
                    "guide": "결과가 많습니다. 상위 3개만 언급하고 조건 좁히기를 제안하세요."}));
            }
            if data
                .get("search_meta")
                .and_then(|m| m.get("semantic_used"))
                .and_then(|v| v.as_bool())
                .unwrap_or(false)
            {
                hints.push(json!({"situation": "semantic_enriched",
                    "guide": "의미 검색이 추가 결과를 포함합니다."}));
            }
            if data.get("_auto_brief").is_some() {
                hints.push(json!({"situation": "fuzzy_matched",
                    "guide": "정확한 검색 결과는 없지만 유사 프로젝트가 _auto_brief에 있습니다. 이 정보로 답변하세요."}));
            }
        }
        "urgent" => {
            let critical = data.get("critical").and_then(|v| v.as_i64()).unwrap_or(0);
            let total = data.get("total").and_then(|v| v.as_i64()).unwrap_or(0);
            if critical > 0 {
                hints.push(json!({"situation": "has_critical", "tone": "alert",
                    "guide": "긴급 프로젝트가 있습니다. 이것을 먼저 언급하세요."}));
            } else if total == 0 {
                hints.push(json!({"situation": "all_clear", "tone": "reassuring",
                    "guide": "긴급 항목이 없습니다. 짧게 '현재 긴급한 것은 없습니다'라고 답하세요."}));
            }
        }
        "brief" => {
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
        }
        "person" => {
            let count = data
                .get("project_count")
                .and_then(|v| v.as_i64())
                .unwrap_or(0);
            if count >= 5 {
                hints.push(json!({"situation": "overloaded",
                    "guide": format!("이 인물이 {}개 프로젝트를 담당합니다. 과부하 상태임을 언급하세요.", count)}));
            }
        }
        "show" => {
            let pid = data
                .get("id")
                .or(data.get("project").and_then(|p| p.get("id")))
                .and_then(|v| v.as_i64())
                .unwrap_or(0);
            if pid > 0 {
                hints.push(json!({"situation": "show_detail",
                    "guide": format!("프로젝트 상세입니다. 요약: brief {}, 이력: timeline {}.", pid, pid)}));
            }
        }
        "list" => {
            let count = data
                .get("projects")
                .and_then(|v| v.as_array())
                .map(|a| a.len())
                .unwrap_or(0);
            hints.push(json!({"situation": "project_list",
                "guide": format!("{}개 프로젝트 목록입니다. 상세는 brief <ID>로 확인하세요.", count)}));
        }
        "dashboard" => {
            hints.push(json!({"situation": "dashboard_overview",
                "guide": "전체 현황입니다. 긴급: urgent, 금액: pipeline으로 확인하세요."}));
        }
        "timeline" => {
            hints.push(json!({"situation": "timeline_view",
                "guide": "이력/일정입니다. 시간순으로 핵심 이벤트를 요약하세요."}));
        }
        "pipeline" => {
            hints.push(json!({"situation": "pipeline_view",
                "guide": "금액/수주 현황입니다. 총액과 상위 프로젝트를 먼저 언급하세요."}));
        }
        _ => {}
    }

    json!({
        "command": command,
        "has_results": !data.is_null(),
        "result_type": result_type,
        "hints": hints,
    })
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// E-4: Proactive data bundling
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Build proactive data bundle. Prefetches related data the AI might need next.
/// Port of Python _build_bundle (E-4).
pub fn build_bundle(command: &str, data: &Value, conn: Option<&Connection>) -> Value {
    let mut bundle = json!({});
    let conn = match conn {
        Some(c) => c,
        None => return bundle,
    };

    match command {
        "brief" => {
            let pid = data.get("project_id").and_then(|v| v.as_i64());
            if let Some(pid) = pid {
                // Check overdue actions
                if let Ok(mut stmt) = conn.prepare(
                    "SELECT content FROM chunks WHERE project_id=?1 AND chunk_type='next_action'",
                ) {
                    let today = chrono::Local::now().format("%Y-%m-%d").to_string();
                    if let Ok(rows) =
                        stmt.query_map(rusqlite::params![pid], |r| r.get::<_, String>(0))
                    {
                        let re = Regex::new(r"20\d{2}[-/]\d{2}[-/]\d{2}").expect("valid regex");
                        for content in rows.flatten() {
                            for m in re.find_iter(&content) {
                                let d = m.as_str().replace('/', "-");
                                if d <= today {
                                    bundle["urgency"] = json!({"priority": "overdue", "reason": format!("기한 도래: {}", d)});
                                    break;
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
                                let related: Vec<Value> = rows.flatten().collect();
                                if !related.is_empty() { bundle["related_projects"] = json!(related); }
                            }
                        }
                    }
                }
            }
        }
        "person" => {
            // This week's activity
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
        }
        "show" => {
            // Same-client projects
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
                if let (Some(pid), Some(first_word)) = (show_pid, client.split_whitespace().next())
                {
                    if let Ok(mut stmt) = conn.prepare(
                        "SELECT id, name, status FROM projects WHERE client LIKE ?1 AND id != ?2 LIMIT 3"
                    ) {
                        let pattern = format!("%{}%", crate::utils::escape_like(first_word));
                        if let Ok(rows) = stmt.query_map(rusqlite::params![pattern, pid], |r| {
                            Ok(json!({"id": r.get::<_, i64>(0)?, "name": r.get::<_, String>(1)?, "status": r.get::<_, String>(2)?}))
                        }) {
                            let related: Vec<Value> = rows.flatten().collect();
                            if !related.is_empty() { bundle["same_client_projects"] = json!(related); }
                        }
                    }
                }
            }
        }
        _ => {}
    }

    bundle
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// E-7: Auto-correction
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Try to auto-correct a failed command by re-routing.
/// Port of Python _try_auto_correct with 5 correction patterns.
/// Returns Some((corrected_command, modified_query)) if correction possible.
pub fn try_auto_correct(
    command: &str,
    query: &str,
    data: &Value,
    conn: Option<&Connection>,
) -> Option<(&'static str, String)> {
    let error_msg = data.get("error").and_then(|v| v.as_str()).unwrap_or("");

    // Pattern 1: show/brief/timeline not found → search
    if matches!(command, "show" | "brief" | "timeline") && error_msg.contains("없음") {
        return Some(("search", query.to_string()));
    }

    // Pattern 2: person with a project name → brief
    if command == "person" && !error_msg.is_empty() {
        if let Some(conn) = conn {
            if let Some((pid, _, _)) = crate::utils::find_project_id_in_text(conn, query, 0.55) {
                return Some(("brief", pid.to_string()));
            }
        }
    }

    // Pattern 3: search 0 results + _auto_brief → brief
    if command == "search" {
        let proj_count = data
            .get("result_count")
            .and_then(|rc| rc.get("projects"))
            .and_then(|v| v.as_i64())
            .unwrap_or(-1);
        if proj_count == 0 {
            if let Some(ab) = data.get("_auto_brief") {
                if let Some(pid) = ab.get("project_id").and_then(|v| v.as_i64()) {
                    return Some(("brief", pid.to_string()));
                }
            }
            // Pattern 4: search 0 + suggestions → brief first suggestion
            if let Some(suggestions) = data.get("suggestions").and_then(|v| v.as_array()) {
                for s in suggestions {
                    if s.get("kind").and_then(|v| v.as_str()) == Some("project") {
                        if let Some(pid) = s.get("project_id").and_then(|v| v.as_i64()) {
                            return Some(("brief", pid.to_string()));
                        }
                    }
                }
            }
        }
    }

    // Pattern 5: timeline with text (no project found) → fuzzy retry
    if command == "timeline" && error_msg.contains("없음") {
        if let Some(conn) = conn {
            if let Some((pid, _, _)) = crate::utils::find_project_id_in_text(conn, query, 0.55) {
                return Some(("timeline", pid.to_string()));
            }
        }
    }

    None
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Routing helpers
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Calculate routing confidence for a NL query.
pub fn route_confidence(query: &str) -> (&'static str, f64) {
    let q = query.to_lowercase();
    let high_patterns: &[(&str, &str, f64)] = &[
        (r"급한|긴급|위험|blocked|마감|기한|납기", "urgent", 0.9),
        (r"현황|대시보드|전체.*상태", "dashboard", 0.85),
        (r"연락처|전화번호|이메일", "contacts", 0.9),
        (r"금액|매출|파이프라인|수주", "pipeline", 0.85),
        (r"주간|이번.*주|보고", "weekly", 0.8),
        (r"비교|차이|다른점", "compare", 0.85),
        (r"타임라인|이력|경과", "timeline", 0.8),
        (r"연결고리|관련.*프로젝트|인력.*충돌", "cross", 0.85),
        (r"뭐.*하고.*있|담당하는|포트폴리오", "person", 0.8),
        (r"브리프|한눈에.*요약", "brief", 0.8),
    ];
    for (pattern, cmd, conf) in high_patterns {
        if let Ok(re) = Regex::new(pattern) {
            if re.is_match(&q) {
                return (cmd, *conf);
            }
        }
    }
    ("search", 0.5)
}

/// Smart route: low-confidence search with project-like query → brief.
pub fn smart_route(query: &str) -> (&'static str, f64) {
    let (cmd, conf) = route_confidence(query);
    if cmd == "search" && conf < 0.6 {
        if let Ok(re) = Regex::new(r"어떻게|어때|상황|진행|되고\s*있|근황") {
            if re.is_match(query) {
                return ("brief", 0.7);
            }
        }
    }
    (cmd, conf)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Output format
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Apply output format transformation.
/// Port of Python _apply_format: summary, detail, markdown, ids.
pub fn apply_format(data: &Value, _command: &str, fmt: &str) -> Value {
    match fmt {
        "ids" => {
            let projects = data.get("projects").and_then(|v| v.as_array());
            if let Some(projects) = projects {
                let ids: Vec<Value> = projects
                    .iter()
                    .filter_map(|p| p.get("id").or(p.get("project_id")))
                    .cloned()
                    .collect();
                json!({"ids": ids})
            } else {
                data.clone()
            }
        }
        "markdown" => {
            let projects = data.get("projects").and_then(|v| v.as_array());
            if let Some(projects) = projects {
                let mut lines = vec![
                    "| ID | 프로젝트 | 상태 | 담당 |".to_string(),
                    "|---|---|---|---|".to_string(),
                ];
                for p in projects {
                    lines.push(format!(
                        "| {} | {} | {} | {} |",
                        p.get("id")
                            .and_then(|v| v.as_i64())
                            .map(|v| v.to_string())
                            .unwrap_or_default(),
                        p.get("name").and_then(|v| v.as_str()).unwrap_or(""),
                        p.get("status").and_then(|v| v.as_str()).unwrap_or(""),
                        p.get("person")
                            .or(p.get("person_internal"))
                            .and_then(|v| v.as_str())
                            .unwrap_or(""),
                    ));
                }
                let mut out = data.clone();
                out["markdown"] = json!(lines.join("\n"));
                out
            } else {
                data.clone()
            }
        }
        "detail" => {
            let projects = data.get("projects").and_then(|v| v.as_array());
            if let Some(projects) = projects {
                let lines: Vec<String> = projects
                    .iter()
                    .map(|p| {
                        format!(
                            "[{}] {} — {}",
                            p.get("id").and_then(|v| v.as_i64()).unwrap_or(0),
                            p.get("name").and_then(|v| v.as_str()).unwrap_or(""),
                            p.get("status").and_then(|v| v.as_str()).unwrap_or(""),
                        )
                    })
                    .collect();
                let mut out = data.clone();
                out["lines"] = json!(lines);
                out
            } else {
                data.clone()
            }
        }
        _ => data.clone(),
    }
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Summary generation
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Generate a Korean summary string for a command result.
pub fn generate_summary(command: &str, data: &Value) -> String {
    match command {
        "search" => {
            let rc = data.get("result_count").unwrap_or(&Value::Null);
            let projects = rc.get("projects").and_then(|v| v.as_i64()).unwrap_or(0);
            let comms = rc
                .get("communications")
                .and_then(|v| v.as_i64())
                .unwrap_or(0);
            let top_names: Vec<String> = data
                .get("projects")
                .and_then(|v| v.as_array())
                .map(|arr| {
                    arr.iter()
                        .take(3)
                        .filter_map(|p| p.get("name").and_then(|v| v.as_str()).map(String::from))
                        .collect()
                })
                .unwrap_or_default();
            if top_names.is_empty() {
                format!(
                    "검색 결과: {}개 프로젝트, {}건 커뮤니케이션",
                    projects, comms
                )
            } else {
                format!(
                    "검색 결과: {} ({}개 프로젝트, {}건 커뮤니케이션)",
                    top_names.join(", "),
                    projects,
                    comms
                )
            }
        }
        "urgent" => {
            let total = data.get("total").and_then(|v| v.as_i64()).unwrap_or(0);
            let critical = data.get("critical").and_then(|v| v.as_i64()).unwrap_or(0);
            format!("관심 필요 {}건 (긴급 {}건)", total, critical)
        }
        "show" => {
            let name = data
                .get("project")
                .and_then(|p| p.get("name"))
                .and_then(|v| v.as_str())
                .unwrap_or("?");
            format!("프로젝트 상세: {}", name)
        }
        "brief" => {
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
        "dashboard" => {
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
        "person" => {
            let name = data.get("person").and_then(|v| v.as_str()).unwrap_or("?");
            let count = data
                .get("project_count")
                .and_then(|v| v.as_i64())
                .unwrap_or(0);
            format!("{}: 프로젝트 {}개", name, count)
        }
        "pipeline" => {
            let total = data
                .get("total_amount")
                .and_then(|v| v.as_f64())
                .unwrap_or(0.0);
            format!("파이프라인 총 {:.1}억원", total)
        }
        "weekly" => {
            let active = data
                .get("active_projects")
                .and_then(|v| v.as_i64())
                .unwrap_or(0);
            format!("활성 프로젝트 {}개", active)
        }
        _ => format!("{} 완료", command),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_route_confidence() {
        let (cmd, conf) = route_confidence("급한 프로젝트 뭐있어");
        assert_eq!(cmd, "urgent");
        assert!(conf > 0.8);
    }

    #[test]
    fn test_smart_route() {
        let (cmd, _) = smart_route("비금도 어떻게 되고 있어?");
        assert_eq!(cmd, "brief");
    }

    #[test]
    fn test_generate_summary() {
        let data = json!({"total": 5, "critical": 2});
        let summary = generate_summary("urgent", &data);
        assert!(summary.contains("5건"));
    }

    #[test]
    fn test_build_ai_hint_search_no_results() {
        let data = json!({"result_count": {"projects": 0}});
        let hint = build_ai_hint("search", &data);
        let hints = hint.get("hints").unwrap().as_array().unwrap();
        assert!(!hints.is_empty());
        assert_eq!(hints[0].get("situation").unwrap(), "no_results");
    }

    #[test]
    fn test_build_ai_hint_urgent_critical() {
        let data = json!({"critical": 3, "total": 5});
        let hint = build_ai_hint("urgent", &data);
        let hints = hint.get("hints").unwrap().as_array().unwrap();
        assert!(!hints.is_empty());
        assert_eq!(hints[0].get("situation").unwrap(), "has_critical");
    }

    #[test]
    fn test_try_auto_correct_show_not_found() {
        let data = json!({"error": "프로젝트 없음"});
        let result = try_auto_correct("show", "비금도", &data, None);
        assert_eq!(result, Some(("search", "비금도".to_string())));
    }

    #[test]
    fn test_apply_depth_compact_show() {
        let data = json!({"project": {"id": 1, "name": "test", "status": "진행중", "client": "A사"},
                          "sections": [1,2,3], "communications": [1]});
        let compact = apply_depth(&data, "show", "compact");
        assert!(compact.get("section_count").is_some());
    }

    #[test]
    fn test_apply_format_ids() {
        let data = json!({"projects": [{"id": 1}, {"id": 2}]});
        let result = apply_format(&data, "search", "ids");
        let ids = result.get("ids").unwrap().as_array().unwrap();
        assert_eq!(ids.len(), 2);
    }

    #[test]
    fn test_apply_format_markdown() {
        let data =
            json!({"projects": [{"id": 1, "name": "P1", "status": "진행중", "person": "김"}]});
        let result = apply_format(&data, "search", "markdown");
        assert!(result.get("markdown").is_some());
    }
}
