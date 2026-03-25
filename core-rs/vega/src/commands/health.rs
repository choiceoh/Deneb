//! System health check command.

use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// System health check: DB integrity, FTS index, file/chunk counts.
pub fn cmd_health(_args: &Value, config: &VegaConfig) -> CommandResult {
    let mut checks = Vec::new();
    let mut all_ok = true;

    // 1. DB file exists
    let db_exists = config.db_exists();
    checks.push(json!({
        "check": "DB 파일",
        "status": if db_exists { "ok" } else { "fail" },
        "detail": config.db_path.to_string_lossy(),
    }));
    if !db_exists {
        all_ok = false;
    }

    // 2. MD directory exists
    let md_valid = config.md_dir_valid();
    checks.push(json!({
        "check": "MD 디렉토리",
        "status": if md_valid { "ok" } else { "warn" },
        "detail": config.md_dir.to_string_lossy(),
    }));

    // 3. DB integrity and counts
    if db_exists {
        match open_db(config) {
            Ok(conn) => {
                // Integrity check
                let integrity: String = conn
                    .query_row("PRAGMA integrity_check", [], |r| r.get(0))
                    .unwrap_or_else(|_| "error".to_string());
                let integrity_ok = integrity == "ok";
                checks.push(json!({
                    "check": "DB 무결성",
                    "status": if integrity_ok { "ok" } else { "fail" },
                    "detail": integrity,
                }));
                if !integrity_ok {
                    all_ok = false;
                }

                // WAL mode
                let journal: String = conn
                    .query_row("PRAGMA journal_mode", [], |r| r.get(0))
                    .unwrap_or_default();
                checks.push(json!({
                    "check": "저널 모드",
                    "status": if journal == "wal" { "ok" } else { "warn" },
                    "detail": journal,
                }));

                // Table counts
                let project_count: i64 = conn
                    .query_row("SELECT COUNT(*) FROM projects", [], |r| r.get(0))
                    .unwrap_or(0);
                let chunk_count: i64 = conn
                    .query_row("SELECT COUNT(*) FROM chunks", [], |r| r.get(0))
                    .unwrap_or(0);
                let comm_count: i64 = conn
                    .query_row("SELECT COUNT(*) FROM comm_log", [], |r| r.get(0))
                    .unwrap_or(0);
                let tag_count: i64 = conn
                    .query_row("SELECT COUNT(DISTINCT name) FROM tags", [], |r| r.get(0))
                    .unwrap_or(0);

                checks.push(json!({
                    "check": "데이터 현황",
                    "status": "ok",
                    "detail": format!("프로젝트 {}개, 청크 {}개, 커뮤니케이션 {}건, 태그 {}개",
                        project_count, chunk_count, comm_count, tag_count),
                }));

                // FTS index
                let fts_ok: bool = conn
                    .query_row(
                        "SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='chunks_fts'",
                        [],
                        |r| r.get(0),
                    )
                    .unwrap_or(false);
                checks.push(json!({
                    "check": "FTS5 인덱스",
                    "status": if fts_ok { "ok" } else { "warn" },
                    "detail": if fts_ok { "정상" } else { "인덱스 없음 — upgrade 실행 필요" },
                }));

                // Schema version
                let schema_ver: u32 = conn
                    .pragma_query_value(None, "user_version", |r| r.get(0))
                    .unwrap_or(0);
                checks.push(json!({
                    "check": "스키마 버전",
                    "status": if schema_ver >= crate::config::SCHEMA_VERSION { "ok" } else { "warn" },
                    "detail": format!("v{} (최신: v{})", schema_ver, crate::config::SCHEMA_VERSION),
                }));

                // ML availability
                let ml_available = config.has_ml();
                checks.push(json!({
                    "check": "ML 추론",
                    "status": if ml_available { "ok" } else { "info" },
                    "detail": if ml_available {
                        format!("활성 (backend: {})", config.inference_backend)
                    } else {
                        "비활성 (의미검색 없음)".to_string()
                    },
                }));
            }
            Err(e) => {
                checks.push(json!({
                    "check": "DB 연결",
                    "status": "fail",
                    "detail": e,
                }));
                all_ok = false;
            }
        }
    }

    CommandResult::ok(
        "health",
        json!({
            "status": if all_ok { "healthy" } else { "degraded" },
            "checks": checks,
            "version": crate::config::VERSION,
        }),
    )
}
