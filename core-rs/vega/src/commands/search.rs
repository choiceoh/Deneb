//! Search command — full hybrid search with fusion ranking.

use std::collections::HashMap;

use rusqlite::Connection;
use serde_json::{json, Value};

use crate::config::VegaConfig;
use crate::search::SearchRouter;

use super::{truncate, CommandResult};

pub struct SearchHandler;

impl super::CommandHandler for SearchHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_search(args, config)
    }

    fn compact_result(&self, data: &Value) -> Value {
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

    fn ai_hints(&self, data: &Value) -> Vec<Value> {
        let mut hints: Vec<Value> = Vec::new();
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
        hints
    }

    fn summary(&self, data: &Value) -> String {
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
}

/// search: Full hybrid search with fusion ranking.
/// Port of Python vega/commands/search.py with match_reasons, follow_up_hint,
/// suggestions, auto_brief, and communications truncation.
pub(super) fn cmd_search(args: &Value, config: &VegaConfig) -> CommandResult {
    let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
    if query.is_empty() {
        return CommandResult::err("search", "검색어가 필요합니다");
    }

    let router = SearchRouter::new(config.clone());
    match router.search(query) {
        Ok(result) => {
            let query_lower = query.to_lowercase();

            // Group by project
            let mut projects: Vec<Value> = Vec::new();
            let mut seen_pids: HashMap<i64, usize> = HashMap::new();

            for item in &result.unified {
                if let Some(&idx) = seen_pids.get(&item.project_id) {
                    if let Some(proj) = projects.get_mut(idx) {
                        if let Some(sections) = proj.get_mut("sections") {
                            if let Some(arr) = sections.as_array_mut() {
                                arr.push(json!({
                                    "heading": item.heading,
                                    "content": truncate(&item.content, 300),
                                    "type": item.chunk_type,
                                    "date": item.entry_date,
                                    "source": item.source,
                                }));
                            }
                        }
                        // Track sources
                        if let Some(sources) = proj.get_mut("sources") {
                            if let Some(arr) = sources.as_array_mut() {
                                let src = json!(item.source);
                                if !arr.contains(&src) {
                                    arr.push(src);
                                }
                            }
                        }
                        // Update score to max
                        if let Some(cur_score) = proj.get("score").and_then(|v| v.as_f64()) {
                            if item.score > cur_score {
                                proj["score"] = json!(item.score);
                            }
                        }
                    }
                } else {
                    seen_pids.insert(item.project_id, projects.len());
                    projects.push(json!({
                        "id": item.project_id,
                        "name": item.project_name,
                        "client": item.client,
                        "status": item.status,
                        "person": item.person,
                        "score": item.score,
                        "sections": [{
                            "heading": item.heading,
                            "content": truncate(&item.content, 300),
                            "type": item.chunk_type,
                            "date": item.entry_date,
                            "source": item.source,
                        }],
                        "sources": [item.source],
                    }));
                }
            }

            // Add match_reasons per project
            for proj in &mut projects {
                let mut reasons = Vec::new();
                let name_lower = proj
                    .get("name")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_lowercase();

                if !name_lower.is_empty()
                    && name_lower.len() >= 2
                    && (name_lower.contains(&query_lower) || query_lower.contains(&name_lower))
                {
                    reasons.push("프로젝트명");
                }

                let sources: Vec<String> = proj
                    .get("sources")
                    .and_then(|v| v.as_array())
                    .map(|arr| {
                        arr.iter()
                            .filter_map(|v| v.as_str().map(String::from))
                            .collect()
                    })
                    .unwrap_or_default();

                if sources.iter().any(|s| s == "sqlite") {
                    let has_comm = proj
                        .get("sections")
                        .and_then(|v| v.as_array())
                        .map(|arr| {
                            arr.iter()
                                .any(|s| s.get("type").and_then(|v| v.as_str()) == Some("comm_log"))
                        })
                        .unwrap_or(false);
                    if has_comm {
                        reasons.push("커뮤니케이션");
                    }
                    if !has_comm
                        || proj
                            .get("sections")
                            .and_then(|v| v.as_array())
                            .map(|a| a.len())
                            .unwrap_or(0)
                            > 1
                    {
                        reasons.push("본문");
                    }
                }
                if sources.iter().any(|s| s == "semantic") {
                    reasons.push("의미검색");
                }
                if reasons.is_empty() {
                    reasons.push("키워드");
                }
                proj["match_reasons"] = json!(reasons);
            }

            // Matched keywords from analysis
            let extracted = &result.analysis.extracted;
            let mut all_keywords: Vec<String> = Vec::new();
            all_keywords.extend(extracted.keywords.iter().cloned());
            all_keywords.extend(extracted.clients.iter().cloned());
            all_keywords.extend(extracted.persons.iter().cloned());
            all_keywords.extend(extracted.statuses.iter().cloned());
            if all_keywords.is_empty() {
                all_keywords.extend(query.split_whitespace().map(String::from));
            }

            let mut matched_kw: Vec<String> = Vec::new();
            for kw in &all_keywords {
                let kw_lower = kw.to_lowercase();
                for proj in &projects {
                    let text = format!(
                        "{} {}",
                        proj.get("name").and_then(|v| v.as_str()).unwrap_or(""),
                        proj.get("sections")
                            .and_then(|v| v.as_array())
                            .map(|arr| arr
                                .iter()
                                .filter_map(|s| s.get("content").and_then(|v| v.as_str()))
                                .collect::<Vec<_>>()
                                .join(" "))
                            .unwrap_or_default()
                    )
                    .to_lowercase();
                    if text.contains(&kw_lower) && !matched_kw.contains(kw) {
                        matched_kw.push(kw.clone());
                        break;
                    }
                }
            }

            // Communications with truncation info
            let total_comms = result.comms.len();
            let comms: Vec<Value> = result
                .comms
                .iter()
                .take(10)
                .map(|c| {
                    json!({
                        "date": c.log_date,
                        "project": c.name,
                        "sender": c.sender,
                        "subject": c.subject,
                    })
                })
                .collect();

            // Build response
            let mut data = json!({
                "query": query,
                "projects": projects,
                "communications": comms,
                "result_count": {
                    "projects": projects.len(),
                    "communications": comms.len(),
                },
                "matched_keywords": matched_kw,
                "search_meta": result.search_meta,
                "analysis": {
                    "route": result.search_meta.route,
                    "reason": result.analysis.reason,
                },
            });

            // Communications truncation note
            if total_comms > 10 {
                data["communications_total"] = json!(total_comms);
                data["communications_note"] =
                    json!(format!("최신 10건 표시 (전체 {}건)", total_comms));
            }

            // Follow-up hint based on result count
            let proj_count = projects.len();
            if proj_count == 0 {
                // Zero results: suggestions + alternative commands
                if let Ok(conn) = Connection::open(&config.db_path) {
                    let suggestions = crate::utils::build_search_suggestions(&conn, query, 8);
                    if !suggestions.is_empty() {
                        data["suggestions"] =
                            serde_json::to_value(&suggestions).unwrap_or_default();
                    }
                    // Auto-brief fallback via fuzzy match
                    if let Some((fz_pid, _name, fz_conf)) =
                        crate::utils::find_project_id_in_text(&conn, query, 0.6)
                    {
                        if let Ok(auto_brief) = super::brief::build_single_brief(&conn, fz_pid) {
                            let mut ab = auto_brief;
                            ab["_match_confidence"] = json!(fz_conf);
                            data["_auto_brief"] = ab;
                        }
                    }
                }
                data["alternative_commands"] = json!(["list", "dashboard"]);
                data["follow_up_hint"] = json!(
                    "검색 결과 없음. show <ID>, brief <프로젝트명> 형태로 직접 조회해보세요."
                );
            } else if proj_count == 1 {
                let top_id = projects[0].get("id").and_then(|v| v.as_i64()).unwrap_or(0);
                data["follow_up_hint"] = json!(format!(
                    "show {} / brief {} / timeline {}",
                    top_id, top_id, top_id
                ));
                // Auto-brief for single match
                if let Ok(conn) = Connection::open(&config.db_path) {
                    if let Ok(auto_brief) = super::brief::build_single_brief(&conn, top_id) {
                        data["_auto_brief"] = auto_brief;
                    }
                }
            } else if proj_count > 5 {
                let top_id = projects[0].get("id").and_then(|v| v.as_i64()).unwrap_or(0);
                data["follow_up_hint"] = json!(format!(
                    "결과 {}건. show {} / brief {} 로 상세 확인",
                    proj_count, top_id, top_id
                ));
            } else {
                let top_id = projects[0].get("id").and_then(|v| v.as_i64()).unwrap_or(0);
                data["follow_up_hint"] = json!(format!(
                    "show {} / brief {} / timeline {}",
                    top_id, top_id, top_id
                ));
            }

            CommandResult::ok("search", data)
        }
        Err(e) => CommandResult::err("search", &format!("검색 오류: {}", e)),
    }
}
