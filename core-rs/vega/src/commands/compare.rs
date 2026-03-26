use rusqlite::{params, Connection};
use serde_json::{json, Value};

use super::{find_project_id, open_db, CommandResult};
use crate::config::VegaConfig;

/// Compare two projects: shared/unique vendors, materials, persons, tags.
/// Args: { "project_a": "프로젝트A", "project_b": "프로젝트B" }
pub fn cmd_compare(args: &Value, config: &VegaConfig) -> CommandResult {
    let proj_a = match args.get("project_a").and_then(|v| v.as_str()) {
        Some(s) => s.trim(),
        None => return CommandResult::err("compare", "project_a를 입력해주세요"),
    };
    let proj_b = match args.get("project_b").and_then(|v| v.as_str()) {
        Some(s) => s.trim(),
        None => return CommandResult::err("compare", "project_b를 입력해주세요"),
    };

    let id_a = match find_project_id(config, proj_a) {
        Some(id) => id,
        None => {
            return CommandResult::err(
                "compare",
                &format!("프로젝트를 찾을 수 없습니다: {}", proj_a),
            )
        }
    };
    let id_b = match find_project_id(config, proj_b) {
        Some(id) => id,
        None => {
            return CommandResult::err(
                "compare",
                &format!("프로젝트를 찾을 수 없습니다: {}", proj_b),
            )
        }
    };

    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("compare", &e),
    };

    // Compare each dimension
    let vendors = compare_dimension(&conn, id_a, id_b, "vendor");
    let materials = compare_dimension(&conn, id_a, id_b, "material");
    let persons = compare_dimension(&conn, id_a, id_b, "person");
    let tags = compare_tags(&conn, id_a, id_b);

    CommandResult::ok(
        "compare",
        json!({
            "project_a": { "id": id_a, "name": proj_a },
            "project_b": { "id": id_b, "name": proj_b },
            "vendors": vendors,
            "materials": materials,
            "persons": persons,
            "tags": tags,
            "summary": format!(
                "{}와 {} 비교: 공통 업체 {}건, 공통 자재 {}건, 공통 인원 {}건",
                proj_a, proj_b,
                vendors["shared"].as_array().map(|a| a.len()).unwrap_or(0),
                materials["shared"].as_array().map(|a| a.len()).unwrap_or(0),
                persons["shared"].as_array().map(|a| a.len()).unwrap_or(0),
            ),
        }),
    )
}

/// Compare chunks of a given type between two projects.
fn compare_dimension(conn: &Connection, id_a: i64, id_b: i64, chunk_type: &str) -> Value {
    let get_set = |project_id: i64| -> Vec<String> {
        let sql =
            "SELECT DISTINCT TRIM(content) FROM chunks WHERE project_id = ?1 AND chunk_type = ?2";
        let mut stmt = match conn.prepare(sql) {
            Ok(s) => s,
            Err(_) => return Vec::new(),
        };
        let rows = match stmt.query_map(params![project_id, chunk_type], |row| {
            let content: String = row.get(0)?;
            Ok(content)
        }) {
            Ok(r) => r,
            Err(_) => return Vec::new(),
        };
        rows.filter_map(|r| r.ok())
            .filter(|s| !s.is_empty())
            .collect()
    };

    let set_a: std::collections::HashSet<String> = get_set(id_a).into_iter().collect();
    let set_b: std::collections::HashSet<String> = get_set(id_b).into_iter().collect();

    let shared: Vec<&String> = set_a.intersection(&set_b).collect();
    let only_a: Vec<&String> = set_a.difference(&set_b).collect();
    let only_b: Vec<&String> = set_b.difference(&set_a).collect();

    json!({
        "shared": shared,
        "only_a": only_a,
        "only_b": only_b,
    })
}

/// Compare tags between two projects.
fn compare_tags(conn: &Connection, id_a: i64, id_b: i64) -> Value {
    let get_tags = |project_id: i64| -> Vec<String> {
        let sql = "SELECT DISTINCT TRIM(t.name) FROM tags t JOIN project_tags pt ON pt.tag_id = t.id WHERE pt.project_id = ?1";
        let mut stmt = match conn.prepare(sql) {
            Ok(s) => s,
            Err(_) => return Vec::new(),
        };
        let rows = match stmt.query_map(params![project_id], |row| {
            let name: String = row.get(0)?;
            Ok(name)
        }) {
            Ok(r) => r,
            Err(_) => return Vec::new(),
        };
        rows.filter_map(|r| r.ok())
            .filter(|s| !s.is_empty())
            .collect()
    };

    let set_a: std::collections::HashSet<String> = get_tags(id_a).into_iter().collect();
    let set_b: std::collections::HashSet<String> = get_tags(id_b).into_iter().collect();

    let shared: Vec<&String> = set_a.intersection(&set_b).collect();
    let only_a: Vec<&String> = set_a.difference(&set_b).collect();
    let only_b: Vec<&String> = set_b.difference(&set_a).collect();

    json!({
        "shared": shared,
        "only_a": only_a,
        "only_b": only_b,
    })
}

/// Project statistics: counts of projects, chunks, comm_logs, tags.
/// Args: {} (no args needed)
pub fn cmd_stats(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("stats", &e),
    };

    let project_count = count_rows(&conn, "SELECT COUNT(*) FROM projects");
    let active_count = count_rows(
        &conn,
        "SELECT COUNT(*) FROM projects WHERE status NOT LIKE '%완료%' AND status NOT LIKE '%중단%'",
    );
    let chunk_count = count_rows(&conn, "SELECT COUNT(*) FROM chunks");
    let comm_count = count_rows(&conn, "SELECT COUNT(*) FROM comm_log");
    let tag_count = count_rows(&conn, "SELECT COUNT(*) FROM tags");

    // Chunk type breakdown
    let chunk_types = get_chunk_type_counts(&conn);

    // Status breakdown
    let status_groups = get_status_counts(&conn);

    CommandResult::ok(
        "stats",
        json!({
            "projects": {
                "total": project_count,
                "active": active_count,
                "completed": project_count - active_count,
            },
            "chunks": {
                "total": chunk_count,
                "by_type": chunk_types,
            },
            "comm_logs": comm_count,
            "tags": tag_count,
            "status_groups": status_groups,
            "summary": format!(
                "프로젝트 {}건 (활성 {}건), 청크 {}건, 소통 {}건, 태그 {}건",
                project_count, active_count, chunk_count, comm_count, tag_count
            ),
        }),
    )
}

fn count_rows(conn: &Connection, sql: &str) -> i64 {
    conn.query_row(sql, [], |row| row.get(0)).unwrap_or(0)
}

fn get_chunk_type_counts(conn: &Connection) -> Value {
    let sql = "SELECT chunk_type, COUNT(*) FROM chunks GROUP BY chunk_type ORDER BY COUNT(*) DESC";
    let mut stmt = match conn.prepare(sql) {
        Ok(s) => s,
        Err(_) => return json!({}),
    };
    let rows = match stmt.query_map([], |row| {
        let ctype: String = row.get(0)?;
        let cnt: i64 = row.get(1)?;
        Ok((ctype, cnt))
    }) {
        Ok(r) => r,
        Err(_) => return json!({}),
    };

    let mut map = serde_json::Map::new();
    for (ctype, cnt) in rows.flatten() {
        map.insert(ctype, json!(cnt));
    }
    Value::Object(map)
}

fn get_status_counts(conn: &Connection) -> Value {
    let sql = "SELECT status, COUNT(*) FROM projects GROUP BY status ORDER BY COUNT(*) DESC";
    let mut stmt = match conn.prepare(sql) {
        Ok(s) => s,
        Err(_) => return json!({}),
    };
    let rows = match stmt.query_map([], |row| {
        let status: String = row.get(0)?;
        let cnt: i64 = row.get(1)?;
        Ok((status, cnt))
    }) {
        Ok(r) => r,
        Err(_) => return json!({}),
    };

    let mut map = serde_json::Map::new();
    for (status, cnt) in rows.flatten() {
        map.insert(status, json!(cnt));
    }
    Value::Object(map)
}
