//! Vega command registry and NL routing.
//!
//! Port of Python vega/core.py + vega/commands/ — complete command set.
//! All Python commands have been ported to Rust.

pub mod add_action;
pub mod brief;
pub mod changelog;
pub mod compare;
pub mod contacts;
pub mod cross;
pub mod dashboard;
pub mod health;
pub mod list;
pub mod mail_append;
pub mod memory;
pub mod person;
pub mod pipeline;
pub mod recent;
pub mod search;
pub mod show;
pub mod sync_back;
pub mod system;
pub mod tags;
pub mod template;
pub mod timeline;
pub mod update;
pub mod upgrade;
pub mod urgent;
pub mod weekly;

use rustc_hash::FxHashMap;
use std::sync::OnceLock;

use regex::Regex;
use rusqlite::{params, Connection};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};

use crate::config::VegaConfig;
use crate::db::schema::init_db;

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

/// Trait implemented by every command handler.
///
/// The optional AI helper methods (`compact_result`, `ai_hints`, `build_bundle`, summary)
/// replace the centralised `match command { ... }` dispatch in ai.rs so each command
/// owns its own presentation and hint logic.
pub trait CommandHandler: Send + Sync {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult;

    /// E-2: Return a compact / brief representation of a successful result.
    /// Default: return data unchanged.
    fn compact_result(&self, data: &Value) -> Value {
        data.clone()
    }

    /// E-3: Return situational AI hint objects for LLM context injection.
    /// Default: no hints.
    fn ai_hints(&self, data: &Value) -> Vec<Value> {
        let _ = data;
        vec![]
    }

    /// E-4: Return a proactive data bundle (related data the AI might need next).
    /// `conn` is `None` when DB access is unavailable.
    /// Default: empty object.
    fn build_bundle(&self, data: &Value, conn: Option<&Connection>) -> Value {
        let _ = (data, conn);
        json!({})
    }

    /// Generate a short Korean summary string for this command's result.
    /// Default: empty string (caller falls back to generic).
    fn summary(&self, data: &Value) -> String {
        let _ = data;
        String::new()
    }
}

pub(super) struct CommandContext {
    pub conn: Connection,
    pub config: VegaConfig,
}

impl CommandContext {
    pub fn new(config: &VegaConfig) -> Result<Self, String> {
        let conn = open_db(config)?;
        Ok(Self {
            conn,
            config: config.clone(),
        })
    }

    pub fn find_project(&self, query: &str) -> Option<i64> {
        if let Ok(id) = query.parse::<i64>() {
            let exists: bool = self
                .conn
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

        let escaped = crate::utils::escape_like(query);
        let like_result = self
            .conn
            .query_row(
                "SELECT id FROM projects WHERE name LIKE ?1 ESCAPE '\\' LIMIT 1",
                params![format!("%{}%", escaped)],
                |r| r.get::<_, i64>(0),
            )
            .ok();
        if like_result.is_some() {
            return like_result;
        }

        crate::utils::find_project_id_in_text(&self.conn, query, 0.55).map(|(id, _, _)| id)
    }

    pub fn query_project_rows(&self, pid: i64, sql: &str) -> Result<Vec<Value>, String> {
        let mut stmt = self
            .conn
            .prepare(sql)
            .map_err(|e| format!("쿼리 준비 실패({}): {e}", self.config.db_path.display()))?;
        let rows = stmt
            .query_map(params![pid], |r| {
                let mut row = serde_json::Map::new();
                for i in 0..r.as_ref().column_count() {
                    let col_name = r
                        .as_ref()
                        .column_name(i)
                        .map_or_else(|_| format!("col_{i}"), ToString::to_string);
                    let val = match r.get_ref(i)? {
                        rusqlite::types::ValueRef::Null => Value::Null,
                        rusqlite::types::ValueRef::Integer(v) => json!(v),
                        rusqlite::types::ValueRef::Real(v) => json!(v),
                        rusqlite::types::ValueRef::Text(v) => {
                            Value::String(String::from_utf8_lossy(v).to_string())
                        }
                        rusqlite::types::ValueRef::Blob(_) => Value::String("<blob>".to_string()),
                    };
                    row.insert(col_name, val);
                }
                Ok(Value::Object(row))
            })
            .map_err(|e| format!("쿼리 실행 실패({}): {e}", self.config.db_path.display()))?;

        Ok(rows.filter_map(Result::ok).collect())
    }
}

/// Route patterns: NL query → command name.
/// Full port of Python `NL_ROUTES` from core.py.
static ROUTE_PATTERNS: &[(&str, &str)] = &[
    // 긴급/관심 필요
    (r"급한|긴급|위험|관심.*필요|우선순위|blocked|막힌", "urgent"),
    (r"마감|기한|납기|데드라인|언제까지", "urgent"),
    (
        r"이번\s*달|다음\s*달|할\s*일|할일|액션\s*아이템|해야.*할",
        "urgent",
    ),
    // 크로스 분석
    (
        r"연결고리|관련.*프로젝트|같은.*거래처|같은.*자재|인력.*충돌|시너지",
        "cross",
    ),
    (r"인력|팀\s*현황|인원|리소스", "cross"),
    // 브리프/최근
    (r"브리프|한눈에.*요약|빠른.*요약|간단.*브리핑", "brief"),
    (
        r"최근.*활동|최근.*업데이트|최근.*변화|최신.*활동|latest",
        "recent",
    ),
    // 비교/통계
    (r"비교|차이|다른점|공통점", "compare"),
    (r"통계|분석|수치|평균|활동량|빈도", "stats"),
    // 대시보드
    (r"현황|대시보드|전체.*상태|프로젝트.*몇|요약", "dashboard"),
    // 인물
    (
        r"뭐\s*하고\s*있|맡은\s*거|담당하는|포트폴리오|업무.*현황",
        "person",
    ),
    // 연락처
    (r"연락처|전화번호|이메일|담당자.*연락", "contacts"),
    // 파이프라인
    (
        r"금액|매출|파이프라인|수주.*금|얼마|비용|원가|예산|단가|견적",
        "pipeline",
    ),
    // 주간/변경
    (r"주간|이번.*주|리포트|보고", "weekly"),
    (r"뭐.*바뀌|변경", "changelog"),
    // 타임라인/일정
    (r"타임라인|이력|경과|순서|일정|스케줄|공정", "timeline"),
    // 프로젝트 목록
    (
        r"프로젝트.*목록|프로젝트.*리스트|전체.*프로젝트|몇.*개",
        "list",
    ),
    // 문제/이슈
    (r"문제|이슈|결함|불량|장애|고장", "search"),
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

static REGISTRY: OnceLock<FxHashMap<&'static str, Box<dyn CommandHandler>>> = OnceLock::new();

fn build_registry() -> FxHashMap<&'static str, Box<dyn CommandHandler>> {
    let mut m: FxHashMap<&'static str, Box<dyn CommandHandler>> = FxHashMap::default();
    m.insert("search", Box::new(search::SearchHandler));
    m.insert("show", Box::new(show::ShowHandler));
    m.insert("brief", Box::new(brief::BriefHandler));
    m.insert("system", Box::new(system::SystemHandler));
    m.insert("upgrade", Box::new(upgrade::UpgradeHandler));
    m.insert("list", Box::new(list::ListHandler));
    m.insert("tags", Box::new(tags::TagsHandler));
    m.insert("timeline", Box::new(timeline::TimelineHandler));
    m.insert("ask", Box::new(AskHandler));
    m.insert("urgent", Box::new(urgent::UrgentHandler));
    m.insert("person", Box::new(person::PersonHandler));
    m.insert("compare", Box::new(compare::CompareHandler));
    m.insert("stats", Box::new(compare::StatsHandler));
    m.insert("recent", Box::new(recent::RecentHandler));
    m.insert("cross", Box::new(cross::CrossHandler));
    m.insert("contacts", Box::new(contacts::ContactsHandler));
    m.insert("dashboard", Box::new(dashboard::DashboardHandler));
    m.insert("pipeline", Box::new(pipeline::PipelineHandler));
    m.insert("weekly", Box::new(weekly::WeeklyHandler));
    m.insert("changelog", Box::new(changelog::ChangelogHandler));
    m.insert("add-action", Box::new(add_action::AddActionHandler));
    m.insert("mail-append", Box::new(mail_append::MailAppendHandler));
    m.insert("update", Box::new(update::UpdateHandler));
    m.insert("template", Box::new(template::TemplateHandler));
    m.insert("sync-back", Box::new(sync_back::SyncBackHandler));
    m.insert("health", Box::new(health::HealthHandler));
    m.insert("memory-search", Box::new(memory::MemorySearchHandler));
    m.insert("memory-update", Box::new(memory::MemoryUpdateHandler));
    m.insert("memory-status", Box::new(memory::MemoryStatusHandler));
    m
}

/// Execute a Vega command by name.
pub fn execute(command: &str, args: &Value, config: &VegaConfig) -> CommandResult {
    let registry = REGISTRY.get_or_init(build_registry);
    if let Some(handler) = registry.get(command) {
        return handler.execute(config, args);
    }
    // Try to route the command as a query
    let routed = route_command(command);
    if routed != command && routed != "search" {
        execute(routed, args, config)
    } else {
        // Default to search with command as query
        let search_args = json!({"query": command});
        registry.get("search").map_or_else(
            || CommandResult::err("search", "검색 핸들러 없음"),
            |h| h.execute(config, &search_args),
        )
    }
}

/// ask: Unified NL endpoint — full E-1 through E-7 framework.
/// E-1: NL routing, E-2: depth, E-3: AI hints, E-4: bundle, E-5: session, E-7: auto-correct.
struct AskHandler;

impl CommandHandler for AskHandler {
    fn execute(&self, config: &VegaConfig, args: &Value) -> CommandResult {
        cmd_ask(args, config)
    }
}

fn cmd_ask(args: &Value, config: &VegaConfig) -> CommandResult {
    let query = args.get("query").and_then(|v| v.as_str()).unwrap_or("");
    if query.is_empty() {
        return CommandResult::err("ask", "query 파라미터가 필요합니다");
    }

    let depth = args
        .get("depth")
        .and_then(|v| v.as_str())
        .unwrap_or("normal");
    let fmt = args
        .get("format")
        .and_then(|v| v.as_str())
        .unwrap_or("summary");

    // E-5: Session-based pronoun resolution
    let resolved_query = {
        let session_path = crate::session::VegaSession::session_path(&config.db_path);
        let session = crate::session::VegaSession::load(&session_path);
        if let Ok(conn) = Connection::open(&config.db_path) {
            session
                .resolve_pronouns(query, &conn)
                .unwrap_or_else(|| query.to_string())
        } else {
            query.to_string()
        }
    };

    // E-1: Smart route
    let (command, confidence) = crate::ai::smart_route(&resolved_query);
    let command = if command == "ask" { "search" } else { command };

    // Execute inner command
    let inner_args = json!({"query": resolved_query});
    let mut result = execute(command, &inner_args, config);

    // E-7: Auto-correction on failure or 0 results
    if !result.success
        || (command == "search"
            && result
                .data
                .get("result_count")
                .and_then(|rc| rc.get("projects"))
                .and_then(serde_json::Value::as_i64)
                .unwrap_or(-1)
                == 0)
    {
        let conn = Connection::open(&config.db_path).ok();
        if let Some((corrected_cmd, corrected_query)) =
            crate::ai::try_auto_correct(command, &resolved_query, &result.data, conn.as_ref())
        {
            let corrected_args = json!({"query": corrected_query});
            let corrected = execute(corrected_cmd, &corrected_args, config);
            if corrected.success {
                result = corrected;
                if let Some(obj) = result.data.as_object_mut() {
                    obj.insert(
                        "_meta".into(),
                        json!({
                            "routed_command": command,
                            "auto_corrected_to": corrected_cmd,
                            "original_query": query,
                            "resolved_query": resolved_query,
                            "confidence": confidence,
                        }),
                    );
                }
                // E-5: Update session with corrected result
                let session_path = crate::session::VegaSession::session_path(&config.db_path);
                let mut session = crate::session::VegaSession::load(&session_path);
                session.update(corrected_cmd, &result.data);
                let _ = session.save(&session_path);
                return result;
            }
        }
    }

    let registry = REGISTRY.get_or_init(build_registry);

    // E-2: Apply depth via per-command trait method
    if depth != "normal" && result.success {
        if let Some(handler) = registry.get(command) {
            if matches!(depth, "compact" | "brief") {
                result.data = handler.compact_result(&result.data);
            }
        }
    }

    // E-6: Apply format
    if fmt != "summary" && result.success {
        result.data = crate::ai::apply_format(&result.data, command, fmt);
    }

    if result.success {
        // E-3: AI behavioral hints via per-command trait method
        let (ai_hint, has_hints) = if let Some(handler) = registry.get(command) {
            let hints = handler.ai_hints(&result.data);
            let has = !hints.is_empty();
            (
                json!({
                    "command": command,
                    "has_results": !result.data.is_null(),
                    "hints": hints,
                }),
                has,
            )
        } else {
            (json!({}), false)
        };

        // E-4: Proactive data bundle via per-command trait method
        let bundle = if depth != "brief" {
            if let Some(handler) = registry.get(command) {
                let conn = Connection::open(&config.db_path).ok();
                let b = handler.build_bundle(&result.data, conn.as_ref());
                if b.as_object().is_some_and(|o| !o.is_empty()) {
                    Some(b)
                } else {
                    None
                }
            } else {
                None
            }
        } else {
            None
        };

        if let Some(obj) = result.data.as_object_mut() {
            obj.insert(
                "_meta".into(),
                json!({
                    "routed_command": command,
                    "original_query": query,
                    "resolved_query": resolved_query,
                    "confidence": confidence,
                }),
            );
            if has_hints {
                obj.insert("_ai_hint".into(), ai_hint);
            }
            if let Some(b) = bundle {
                obj.insert("_bundle".into(), b);
            }
        }

        // E-5: Update session
        let session_path = crate::session::VegaSession::session_path(&config.db_path);
        let mut session = crate::session::VegaSession::load(&session_path);
        session.update(command, &result.data);
        let _ = session.save(&session_path);
    }

    result
}

// -- Helpers --

pub(super) fn open_db(config: &VegaConfig) -> Result<Connection, String> {
    let conn = Connection::open(&config.db_path).map_err(|e| format!("DB 열기 실패: {e}"))?;
    init_db(&conn).map_err(|e| format!("스키마 초기화 실패: {e}"))?;
    Ok(conn)
}

pub(super) fn find_project_id(config: &VegaConfig, query: &str) -> Option<i64> {
    let ctx = CommandContext::new(config).ok()?;
    ctx.find_project(query)
}

pub(super) fn truncate(s: &str, max: usize) -> String {
    if s.chars().count() <= max {
        s.to_string()
    } else {
        let truncated: String = s.chars().take(max).collect();
        format!("{truncated}...")
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
