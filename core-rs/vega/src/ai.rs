//! AI helpers for Vega — routing, auto-correction, output formatting.
//!
//! This module contains only the generic, command-agnostic AI utilities.
//! Per-command depth filtering, hints, bundle generation, and summary
//! strings have been moved to each command's `CommandHandler` trait impl
//! (`compact_result`, `ai_hints`, `build_bundle`, summary) so each command owns
//! its own presentation logic.

use regex::Regex;
use rusqlite::Connection;
use serde_json::{json, Value};

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
// E-7: Auto-correction
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Try to auto-correct a failed command by re-routing.
/// Port of Python _`try_auto_correct` with 5 correction patterns.
/// Returns `Some((corrected_command`, `modified_query`)) if correction possible.
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
            .and_then(serde_json::Value::as_i64)
            .unwrap_or(-1);
        if proj_count == 0 {
            if let Some(ab) = data.get("_auto_brief") {
                if let Some(pid) = ab.get("project_id").and_then(serde_json::Value::as_i64) {
                    return Some(("brief", pid.to_string()));
                }
            }
            // Pattern 4: search 0 + suggestions → brief first suggestion
            if let Some(suggestions) = data.get("suggestions").and_then(|v| v.as_array()) {
                for s in suggestions {
                    if s.get("kind").and_then(|v| v.as_str()) == Some("project") {
                        if let Some(pid) = s.get("project_id").and_then(serde_json::Value::as_i64) {
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
// E-6: Output format
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

/// Apply output format transformation.
/// Port of Python _`apply_format`: summary, detail, markdown, ids.
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
                            .and_then(serde_json::Value::as_i64)
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
                            p.get("id").and_then(serde_json::Value::as_i64).unwrap_or(0),
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
