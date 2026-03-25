//! Vega command registry and NL routing.
//!
//! Port of Python vega/core.py + vega/commands/ — priority commands.
//! Commands: search, show, brief, upgrade, system, list, tags, timeline.

use std::collections::HashMap;

use regex::Regex;
use rusqlite::{params, Connection};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::config::VegaConfig;
use crate::db::schema::init_db;
use crate::search::SearchRouter;

/// Command execution result.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommandResult {
    pub command: String,
    pub success: bool,
    pub data: Value,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

impl CommandResult {
    fn ok(command: &str, data: Value) -> Self {
        Self {
            command: command.into(),
            success: true,
            data,
            error: None,
        }
    }

    fn err(command: &str, msg: &str) -> Self {
        Self {
            command: command.into(),
            success: false,
            data: Value::Null,
            error: Some(msg.into()),
        }
    }
}

/// Route patterns: NL query → command name.
static ROUTE_PATTERNS: &[(&str, &str)] = &[
    (r"급한|긴급|위험|우선순위|blocked", "urgent"),
    (r"연결고리|관련.*프로젝트|인력.*충돌", "cross"),
    (r"브리프|한눈에.*요약", "brief"),
    (r"최근.*활동|latest", "recent"),
    (r"비교|차이", "compare"),
    (r"현황|대시보드|전체.*상태", "dashboard"),
    (r"뭐.*하고.*있|담당하는|포트폴리오", "person"),
    (r"연락처|전화번호|담당자.*연락", "contacts"),
    (r"금액|매출|파이프라인", "pipeline"),
    (r"타임라인|이력|일정", "timeline"),
];

/// Route a natural language query to a command name.
pub fn route_command(query: &str) -> &'static str {
    let q_lower = query.to_lowercase();
    for (pattern, cmd) in ROUTE_PATTERNS {
        if let Ok(re) = Regex::new(pattern) {
            if re.is_match(&q_lower) {
                return cmd;
            }
        }
    }
    "search"
}

/// Execute a Vega command by name.
pub fn execute(command: &str, args: &Value, config: &VegaConfig) -> CommandResult {
    match command {
        "search" => cmd_search(args, config),
        "show" => cmd_show(args, config),
        "brief" => cmd_brief(args, config),
        "system" => cmd_system(args, config),
        "upgrade" => cmd_upgrade(args, config),
        "list" => cmd_list(args, config),
        "tags" => cmd_tags(args, config),
        "timeline" => cmd_timeline(args, config),
        "embed" => cmd_embed(args, config),
        _ => {
            // Try to route the command as a query
            let routed = route_command(command);
            if routed != command && routed != "search" {
                execute(routed, args, config)
            } else {
                // Default to search with command as query
                let search_args = json!({"query": command});
                cmd_search(&search_args, config)
            }
        }
    }
}

/// Execute a NL query: route → command → execute.
pub fn execute_query(query: &str, config: &VegaConfig) -> CommandResult {
    let command = route_command(query);
    let args = json!({"query": query});
    execute(command, &args, config)
}

// -- Command implementations --

/// search: Full hybrid search with fusion ranking.
fn cmd_search(args: &Value, config: &VegaConfig) -> CommandResult {
    let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
    if query.is_empty() {
        return CommandResult::err("search", "검색어가 필요합니다");
    }

    let router = SearchRouter::new(config.clone());
    match router.search(query) {
        Ok(result) => {
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
                                }));
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
                        }],
                        "sources": [item.source],
                    }));
                }
            }

            // Comms
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

            CommandResult::ok(
                "search",
                json!({
                    "projects": projects,
                    "communications": comms,
                    "result_count": {
                        "projects": projects.len(),
                        "communications": comms.len(),
                    },
                    "search_meta": result.search_meta,
                    "analysis": {
                        "route": result.search_meta.route,
                        "reason": result.analysis.reason,
                    },
                }),
            )
        }
        Err(e) => CommandResult::err("search", &format!("검색 오류: {}", e)),
    }
}

/// show: Display detailed project info.
fn cmd_show(args: &Value, config: &VegaConfig) -> CommandResult {
    let project_id = args
        .get("id")
        .or_else(|| args.get("project_id"))
        .and_then(|v| v.as_i64());

    let project_id = match project_id {
        Some(id) => id,
        None => {
            let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
            if query.is_empty() {
                return CommandResult::err("show", "프로젝트 ID 또는 이름이 필요합니다");
            }
            match find_project_id(config, query) {
                Some(id) => id,
                None => return CommandResult::err("show", &format!("프로젝트 '{}' 없음", query)),
            }
        }
    };

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("show", &e),
    };

    let proj = conn.query_row(
        "SELECT id, name, client, status, capacity, biz_type, person_internal, person_external, partner
         FROM projects WHERE id=?1",
        params![project_id],
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
    );

    let proj = match proj {
        Ok(p) => p,
        Err(_) => return CommandResult::err("show", &format!("프로젝트 ID {} 없음", project_id)),
    };

    // Chunks
    let mut stmt = conn
        .prepare("SELECT section_heading, content, chunk_type, entry_date FROM chunks WHERE project_id=?1 ORDER BY id")
        .unwrap();
    let chunks: Vec<Value> = stmt
        .query_map(params![project_id], |r| {
            Ok(json!({
                "heading": r.get::<_, Option<String>>(0)?,
                "content": r.get::<_, Option<String>>(1)?,
                "type": r.get::<_, Option<String>>(2)?,
                "date": r.get::<_, Option<String>>(3)?,
            }))
        })
        .unwrap()
        .filter_map(|r| r.ok())
        .collect();

    // Tags
    let mut stmt = conn
        .prepare(
            "SELECT DISTINCT t.name FROM tags t
             JOIN chunk_tags ct ON ct.tag_id = t.id
             JOIN chunks c ON c.id = ct.chunk_id
             WHERE c.project_id = ?1",
        )
        .unwrap();
    let tags: Vec<String> = stmt
        .query_map(params![project_id], |r| r.get(0))
        .unwrap()
        .filter_map(|r| r.ok())
        .collect();

    // Recent comms
    let mut stmt = conn
        .prepare("SELECT log_date, sender, subject, summary FROM comm_log WHERE project_id=?1 ORDER BY log_date DESC LIMIT 10")
        .unwrap();
    let comms: Vec<Value> = stmt
        .query_map(params![project_id], |r| {
            Ok(json!({
                "date": r.get::<_, Option<String>>(0)?,
                "sender": r.get::<_, Option<String>>(1)?,
                "subject": r.get::<_, Option<String>>(2)?,
                "summary": r.get::<_, Option<String>>(3)?,
            }))
        })
        .unwrap()
        .filter_map(|r| r.ok())
        .collect();

    CommandResult::ok(
        "show",
        json!({
            "project": proj,
            "sections": chunks,
            "tags": tags,
            "communications": comms,
        }),
    )
}

/// brief: Quick project overview summary.
fn cmd_brief(args: &Value, config: &VegaConfig) -> CommandResult {
    let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
    if query.is_empty() {
        return CommandResult::err("brief", "검색어가 필요합니다");
    }

    let router = SearchRouter::new(config.clone());
    match router.search(query) {
        Ok(result) => {
            let briefs: Vec<Value> = result
                .project_scores
                .iter()
                .take(5)
                .map(|ps| {
                    let sections: Vec<&_> = result
                        .unified
                        .iter()
                        .filter(|u| u.project_id == ps.project_id)
                        .take(2)
                        .collect();
                    json!({
                        "id": ps.project_id,
                        "name": ps.project_name,
                        "score": ps.score,
                        "summary": sections.first().map(|s| truncate(&s.content, 200)).unwrap_or_default(),
                    })
                })
                .collect();

            CommandResult::ok("brief", json!({ "briefs": briefs }))
        }
        Err(e) => CommandResult::err("brief", &format!("검색 오류: {}", e)),
    }
}

/// system: Database health and stats.
fn cmd_system(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("system", &e),
    };

    let stats: (i64, i64, i64, i64) = conn
        .query_row(
            "SELECT
                (SELECT COUNT(*) FROM projects),
                (SELECT COUNT(*) FROM chunks),
                (SELECT COUNT(*) FROM comm_log),
                (SELECT COUNT(DISTINCT name) FROM tags)",
            [],
            |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?, r.get(3)?)),
        )
        .unwrap_or((0, 0, 0, 0));

    let ver: u32 = conn
        .pragma_query_value(None, "user_version", |r| r.get(0))
        .unwrap_or(0);

    CommandResult::ok(
        "system",
        json!({
            "version": crate::config::VERSION,
            "schema_version": ver,
            "db_path": config.db_path.to_string_lossy(),
            "md_dir": config.md_dir.to_string_lossy(),
            "rerank_mode": config.rerank_mode,
            "inference_backend": config.inference_backend,
            "stats": {
                "projects": stats.0,
                "chunks": stats.1,
                "communications": stats.2,
                "tags": stats.3,
            },
        }),
    )
}

/// upgrade: Run schema migrations and FTS rebuild.
fn cmd_upgrade(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("upgrade", &e),
    };

    let ver_before: u32 = conn
        .pragma_query_value(None, "user_version", |r| r.get(0))
        .unwrap_or(0);

    if let Err(e) = init_db(&conn) {
        return CommandResult::err("upgrade", &format!("스키마 업그레이드 실패: {}", e));
    }

    if let Err(e) = crate::db::schema::rebuild_fts(&conn) {
        return CommandResult::err("upgrade", &format!("FTS 리빌드 실패: {}", e));
    }

    let ver_after: u32 = conn
        .pragma_query_value(None, "user_version", |r| r.get(0))
        .unwrap_or(0);

    CommandResult::ok(
        "upgrade",
        json!({
            "schema_before": ver_before,
            "schema_after": ver_after,
            "fts_rebuilt": true,
        }),
    )
}

/// list: List all projects.
fn cmd_list(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("list", &e),
    };

    let mut stmt = conn
        .prepare(
            "SELECT p.id, p.name, p.client, p.status, p.person_internal, p.capacity,
                    (SELECT COUNT(*) FROM chunks WHERE project_id=p.id) as chunks,
                    (SELECT COUNT(*) FROM comm_log WHERE project_id=p.id) as comms
             FROM projects p ORDER BY p.id",
        )
        .unwrap();

    let projects: Vec<Value> = stmt
        .query_map([], |r| {
            Ok(json!({
                "id": r.get::<_, i64>(0)?,
                "name": r.get::<_, Option<String>>(1)?,
                "client": r.get::<_, Option<String>>(2)?,
                "status": r.get::<_, Option<String>>(3)?,
                "person": r.get::<_, Option<String>>(4)?,
                "capacity": r.get::<_, Option<String>>(5)?,
                "chunks": r.get::<_, i64>(6)?,
                "comms": r.get::<_, i64>(7)?,
            }))
        })
        .unwrap()
        .filter_map(|r| r.ok())
        .collect();

    CommandResult::ok("list", json!({ "projects": projects }))
}

/// tags: List all tags with project counts.
fn cmd_tags(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("tags", &e),
    };

    let mut stmt = conn
        .prepare(
            "SELECT t.name, COUNT(DISTINCT c.project_id) as cnt
             FROM tags t
             JOIN chunk_tags ct ON ct.tag_id = t.id
             JOIN chunks c ON c.id = ct.chunk_id
             GROUP BY t.name ORDER BY t.name",
        )
        .unwrap();

    let tags: Vec<Value> = stmt
        .query_map([], |r| {
            Ok(json!({
                "name": r.get::<_, String>(0)?,
                "project_count": r.get::<_, i64>(1)?,
            }))
        })
        .unwrap()
        .filter_map(|r| r.ok())
        .collect();

    CommandResult::ok("tags", json!({ "tags": tags }))
}

/// timeline: Project communication timeline.
fn cmd_timeline(args: &Value, config: &VegaConfig) -> CommandResult {
    let project_id = args
        .get("id")
        .or_else(|| args.get("project_id"))
        .and_then(|v| v.as_i64());

    let project_id = match project_id {
        Some(id) => id,
        None => {
            let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
            match find_project_id(config, query) {
                Some(id) => id,
                None => {
                    return CommandResult::err("timeline", "프로젝트 ID 또는 이름이 필요합니다")
                }
            }
        }
    };

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("timeline", &e),
    };

    let name: String = conn
        .query_row(
            "SELECT name FROM projects WHERE id=?1",
            params![project_id],
            |r| r.get(0),
        )
        .unwrap_or_default();

    let mut stmt = conn
        .prepare("SELECT log_date, sender, subject, summary FROM comm_log WHERE project_id=?1 ORDER BY log_date DESC")
        .unwrap();

    let entries: Vec<Value> = stmt
        .query_map(params![project_id], |r| {
            Ok(json!({
                "date": r.get::<_, Option<String>>(0)?,
                "sender": r.get::<_, Option<String>>(1)?,
                "subject": r.get::<_, Option<String>>(2)?,
                "summary": r.get::<_, Option<String>>(3)?,
            }))
        })
        .unwrap()
        .filter_map(|r| r.ok())
        .collect();

    CommandResult::ok(
        "timeline",
        json!({
            "project_id": project_id,
            "project_name": name,
            "entries": entries,
            "count": entries.len(),
        }),
    )
}

/// embed: Generate embeddings for chunks (requires ml feature).
fn cmd_embed(_args: &Value, config: &VegaConfig) -> CommandResult {
    #[cfg(feature = "ml")]
    {
        use crate::search::semantic;

        if !config.has_ml() {
            return CommandResult::err(
                "embed",
                "ML 모델이 설정되지 않았습니다 (VEGA_MODEL_EMBEDDER)",
            );
        }

        let conn = match open_db(config) {
            Ok(c) => c,
            Err(e) => return CommandResult::err("embed", &e),
        };

        // Build ML manager
        let mut ml_configs = Vec::new();
        if let Some(ref path) = config.model_embedder {
            ml_configs.push(deneb_ml::ModelConfig::embedder(
                path.clone(),
                config.model_unload_ttl,
            ));
        }
        let mgr = deneb_ml::ModelManager::new(ml_configs);

        match semantic::embed_chunks(&conn, "qwen3-embedding-8b", &mgr) {
            Ok(count) => CommandResult::ok(
                "embed",
                json!({
                    "embedded_chunks": count,
                }),
            ),
            Err(e) => CommandResult::err("embed", &format!("임베딩 실패: {}", e)),
        }
    }

    #[cfg(not(feature = "ml"))]
    {
        CommandResult::err("embed", "ML 기능이 비활성화되어 있습니다 (feature: ml)")
    }
}

// -- Helpers --

fn open_db(config: &VegaConfig) -> Result<Connection, String> {
    let conn = Connection::open(&config.db_path).map_err(|e| format!("DB 열기 실패: {}", e))?;
    init_db(&conn).map_err(|e| format!("스키마 초기화 실패: {}", e))?;
    Ok(conn)
}

fn find_project_id(config: &VegaConfig, query: &str) -> Option<i64> {
    let conn = Connection::open(&config.db_path).ok()?;
    let _ = init_db(&conn);

    // Try exact ID
    if let Ok(id) = query.parse::<i64>() {
        let exists: bool = conn
            .query_row(
                "SELECT COUNT(*) > 0 FROM projects WHERE id=?1",
                params![id],
                |r| r.get(0),
            )
            .unwrap_or(false);
        if exists {
            return Some(id);
        }
    }

    // Try LIKE search
    conn.query_row(
        "SELECT id FROM projects WHERE name LIKE ?1 LIMIT 1",
        params![format!("%{}%", query)],
        |r| r.get(0),
    )
    .ok()
}

fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        s.to_string()
    } else {
        let truncated: String = s.chars().take(max).collect();
        format!("{}...", truncated)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_route_command() {
        assert_eq!(route_command("급한 프로젝트 뭐있어"), "urgent");
        assert_eq!(route_command("현황 보여줘"), "dashboard");
        assert_eq!(route_command("비금도 검색"), "search");
        assert_eq!(route_command("타임라인 보기"), "timeline");
    }

    #[test]
    fn test_truncate() {
        assert_eq!(truncate("hello", 10), "hello");
        assert_eq!(truncate("hello world", 5), "hello...");
    }
}
