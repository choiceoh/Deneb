//! AI helpers for Vega — LLM-based query processing.
//!
//! Port of Python vega/core.py AI functions:
//! apply_depth, build_ai_hint, build_bundle, try_auto_correct,
//! route_confidence, smart_route, generate_summary.
//!
//! Uses reqwest (blocking) for LLM API calls when available.

use regex::Regex;
use serde_json::{json, Value};

/// Apply depth filter to search results.
/// depth: "compact" (minimal), "normal" (default), "detailed" (full), "full" (everything)
pub fn apply_depth(data: &Value, command: &str, depth: &str) -> Value {
    match depth {
        "compact" => compact_result(data, command),
        "detailed" | "full" => data.clone(), // Full data, no trimming
        _ => data.clone(),                   // "normal" = no change
    }
}

/// Compact result: only top-level summary fields.
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
                                "id": p.get("id"),
                                "name": p.get("name"),
                                "status": p.get("status"),
                                "score": p.get("score"),
                            })
                        })
                        .collect::<Vec<_>>()
                })
                .unwrap_or_default();
            json!({
                "projects": projects,
                "result_count": data.get("result_count"),
            })
        }
        "urgent" => {
            json!({
                "total": data.get("total"),
                "critical": data.get("critical"),
                "overdue": data.get("overdue"),
                "items": data.get("items").and_then(|v| v.as_array()).map(|arr| {
                    arr.iter().take(5).map(|i| json!({
                        "project_name": i.get("project_name"),
                        "priority": i.get("priority"),
                        "reason": i.get("reason"),
                    })).collect::<Vec<_>>()
                }),
            })
        }
        _ => data.clone(),
    }
}

/// Build AI hint for LLM context injection.
/// Returns a structured hint about the command and its results.
pub fn build_ai_hint(command: &str, data: &Value) -> Value {
    json!({
        "command": command,
        "has_results": !data.is_null(),
        "result_type": match command {
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
            _ => "기타",
        },
    })
}

/// Build response bundle with metadata.
pub fn build_bundle(command: &str, data: Value, meta: Option<Value>) -> Value {
    let mut bundle = json!({
        "command": command,
        "data": data,
    });
    if let Some(m) = meta {
        bundle["_meta"] = m;
    }
    bundle
}

/// Calculate routing confidence for a NL query.
/// Returns (command, confidence) where confidence is 0.0–1.0.
pub fn route_confidence(query: &str) -> (&'static str, f64) {
    let q = query.to_lowercase();

    // High-confidence patterns
    let high_patterns: &[(&str, &str, f64)] = &[
        (r"급한|긴급|위험|blocked|마감|기한|납기", "urgent", 0.9),
        (r"현황|대시보드|전체.*상태", "dashboard", 0.85),
        (r"연락처|전화번호|이메일", "contacts", 0.9),
        (r"금액|매출|파이프라인|수주", "pipeline", 0.85),
        (r"주간|이번.*주|보고", "weekly", 0.8),
        (r"비교|차이|다른점", "compare", 0.85),
        (r"타임라인|이력|경과", "timeline", 0.8),
        (
            r"연결고리|관련.*프로젝트|인력.*충돌",
            "cross",
            0.85,
        ),
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

/// Smart route: if confidence is low, try to auto-detect a project name
/// and route to "brief" instead of "search".
pub fn smart_route(query: &str) -> (&'static str, f64) {
    let (cmd, conf) = route_confidence(query);

    if cmd == "search" && conf < 0.6 {
        // Low confidence search — might be a project name query
        // Check if the query looks like it's asking about a specific project
        let project_indicators =
            Regex::new(r"어떻게|어때|상황|진행|되고\s*있|근황").ok();
        if let Some(re) = project_indicators {
            if re.is_match(query) {
                return ("brief", 0.7);
            }
        }
    }

    (cmd, conf)
}

/// Try to auto-correct a failed command by re-routing.
/// Returns Some(corrected_command) if correction is possible.
pub fn try_auto_correct(
    command: &str,
    query: &str,
) -> Option<&'static str> {
    // Only auto-correct search failures
    if command != "search" {
        return None;
    }

    let (alt_cmd, conf) = route_confidence(query);
    if alt_cmd != "search" && conf >= 0.7 {
        return Some(alt_cmd);
    }

    None
}

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
                        .filter_map(|p| p.get("name").and_then(|v| v.as_str()))
                        .map(String::from)
                        .collect()
                })
                .unwrap_or_default();
            if top_names.is_empty() {
                format!("검색 결과: {}개 프로젝트, {}건 커뮤니케이션", projects, comms)
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
            let count = data
                .get("briefs")
                .and_then(|v| v.as_array())
                .map(|a| a.len())
                .unwrap_or(0);
            format!("브리프 {}건", count)
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
}
