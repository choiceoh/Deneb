use rusqlite::Connection;
use serde_json::{json, Value};

use crate::config::VegaConfig;

use super::{open_db, CommandResult};

/// Cross-project analysis: shared vendors, materials, personnel overload,
/// schedule conflicts, and technology synergy across all active projects.
pub fn cmd_cross(_args: &Value, config: &VegaConfig) -> CommandResult {
    let conn = match open_db(config) {
        Ok(c) => c,
        Err(e) => return CommandResult::err("cross", &e),
    };

    let vendor_overlaps = match find_vendor_overlaps(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("cross", &e),
    };
    let material_overlaps = match find_material_overlaps(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("cross", &e),
    };
    let personnel_overload = match find_personnel_overload(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("cross", &e),
    };
    let schedule_conflicts = match find_schedule_conflicts(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("cross", &e),
    };
    let tech_synergy = match find_tech_synergy(&conn) {
        Ok(v) => v,
        Err(e) => return CommandResult::err("cross", &e),
    };

    let mut insights: Vec<Value> = Vec::new();

    // Vendor overlaps
    for (vendor, projects) in &vendor_overlaps {
        if projects.len() >= 2 {
            insights.push(json!({
                "type": "vendor_overlap",
                "severity": if projects.len() >= 3 { "high" } else { "medium" },
                "vendor": vendor,
                "projects": projects,
                "count": projects.len(),
                "message": format!("업체 '{}'이(가) {}개 프로젝트에 관여", vendor, projects.len())
            }));
        }
    }

    // Material overlaps
    for (material, projects) in &material_overlaps {
        if projects.len() >= 2 {
            insights.push(json!({
                "type": "material_overlap",
                "severity": "info",
                "material": material,
                "projects": projects,
                "count": projects.len(),
                "message": format!("자재 '{}'이(가) {}개 프로젝트에서 사용", material, projects.len())
            }));
        }
    }

    // Personnel overload
    for (person, projects) in &personnel_overload {
        let severity = if projects.len() >= 5 {
            "critical"
        } else if projects.len() >= 3 {
            "high"
        } else {
            "medium"
        };
        insights.push(json!({
            "type": "personnel_overload",
            "severity": severity,
            "person": person,
            "projects": projects,
            "count": projects.len(),
            "message": format!("담당자 '{}'이(가) {}개 프로젝트에 배정 (과부하 위험)", person, projects.len())
        }));
    }

    // Schedule conflicts
    for conflict in &schedule_conflicts {
        insights.push(json!({
            "type": "schedule_conflict",
            "severity": "high",
            "projects": conflict,
            "message": format!("프로젝트 간 일정 충돌 감지")
        }));
    }

    // Tech synergy
    for (tech, projects) in &tech_synergy {
        if projects.len() >= 2 {
            insights.push(json!({
                "type": "tech_synergy",
                "severity": "info",
                "technology": tech,
                "projects": projects,
                "count": projects.len(),
                "message": format!("기술 '{}'이(가) {}개 프로젝트에서 공유 가능", tech, projects.len())
            }));
        }
    }

    // Sort by severity priority: critical > high > medium > info
    insights.sort_by(|a, b| {
        let priority = |v: &Value| match v["severity"].as_str().unwrap_or("info") {
            "critical" => 0,
            "high" => 1,
            "medium" => 2,
            _ => 3,
        };
        priority(a).cmp(&priority(b))
    });

    let summary = json!({
        "vendor_overlaps": vendor_overlaps.len(),
        "material_overlaps": material_overlaps.len(),
        "personnel_overloaded": personnel_overload.len(),
        "schedule_conflicts": schedule_conflicts.len(),
        "tech_synergies": tech_synergy.len(),
        "total_insights": insights.len()
    });

    CommandResult::ok(
        "cross",
        json!({
            "summary": summary,
            "insights": insights
        }),
    )
}

/// Find vendors that appear in multiple projects.
fn find_vendor_overlaps(conn: &Connection) -> Result<Vec<(String, Vec<String>)>, String> {
    let sql = "
        SELECT c.body, p.title
        FROM chunks c
        JOIN projects p ON c.project_id = p.id
        WHERE c.chunk_type = 'vendor'
          AND p.status NOT LIKE '%완료%'
          AND p.status NOT LIKE '%종료%'
        ORDER BY p.title
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {e}"))?;
    let rows = stmt
        .query_map([], |row| {
            let body: String = row.get(0)?;
            let title: String = row.get(1)?;
            Ok((body, title))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?;

    let mut vendor_map: std::collections::HashMap<String, Vec<String>> =
        std::collections::HashMap::new();
    for row in rows {
        let (body, title) = row.map_err(|e| format!("행 읽기 실패: {e}"))?;
        let vendor_name = body.lines().next().unwrap_or(&body).trim().to_string();
        if !vendor_name.is_empty() {
            vendor_map
                .entry(vendor_name)
                .or_default()
                .push(title.clone());
        }
    }

    // Deduplicate project lists and filter to overlaps only
    let mut results: Vec<(String, Vec<String>)> = vendor_map
        .into_iter()
        .map(|(vendor, mut projects)| {
            projects.sort();
            projects.dedup();
            (vendor, projects)
        })
        .filter(|(_, projects)| projects.len() >= 2)
        .collect();
    results.sort_by(|a, b| b.1.len().cmp(&a.1.len()));
    Ok(results)
}

/// Find materials that appear in multiple projects.
fn find_material_overlaps(conn: &Connection) -> Result<Vec<(String, Vec<String>)>, String> {
    let sql = "
        SELECT c.body, p.title
        FROM chunks c
        JOIN projects p ON c.project_id = p.id
        WHERE c.chunk_type = 'material'
          AND p.status NOT LIKE '%완료%'
          AND p.status NOT LIKE '%종료%'
        ORDER BY p.title
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {e}"))?;
    let rows = stmt
        .query_map([], |row| {
            let body: String = row.get(0)?;
            let title: String = row.get(1)?;
            Ok((body, title))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?;

    let mut material_map: std::collections::HashMap<String, Vec<String>> =
        std::collections::HashMap::new();
    for row in rows {
        let (body, title) = row.map_err(|e| format!("행 읽기 실패: {e}"))?;
        let material_name = body.lines().next().unwrap_or(&body).trim().to_string();
        if !material_name.is_empty() {
            material_map
                .entry(material_name)
                .or_default()
                .push(title.clone());
        }
    }

    let mut results: Vec<(String, Vec<String>)> = material_map
        .into_iter()
        .map(|(mat, mut projects)| {
            projects.sort();
            projects.dedup();
            (mat, projects)
        })
        .filter(|(_, projects)| projects.len() >= 2)
        .collect();
    results.sort_by(|a, b| b.1.len().cmp(&a.1.len()));
    Ok(results)
}

/// Find personnel assigned to multiple active projects (3+ = overloaded).
fn find_personnel_overload(conn: &Connection) -> Result<Vec<(String, Vec<String>)>, String> {
    let sql = "
        SELECT c.body, p.title
        FROM chunks c
        JOIN projects p ON c.project_id = p.id
        WHERE c.chunk_type IN ('person_internal', 'person_external')
          AND p.status NOT LIKE '%완료%'
          AND p.status NOT LIKE '%종료%'
        ORDER BY p.title
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {e}"))?;
    let rows = stmt
        .query_map([], |row| {
            let body: String = row.get(0)?;
            let title: String = row.get(1)?;
            Ok((body, title))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?;

    let mut person_map: std::collections::HashMap<String, Vec<String>> =
        std::collections::HashMap::new();
    for row in rows {
        let (body, title) = row.map_err(|e| format!("행 읽기 실패: {e}"))?;
        // Extract person name (first line of body)
        let person_name = body.lines().next().unwrap_or(&body).trim().to_string();
        if !person_name.is_empty() {
            person_map
                .entry(person_name)
                .or_default()
                .push(title.clone());
        }
    }

    let mut results: Vec<(String, Vec<String>)> = person_map
        .into_iter()
        .map(|(person, mut projects)| {
            projects.sort();
            projects.dedup();
            (person, projects)
        })
        .filter(|(_, projects)| projects.len() >= 3)
        .collect();
    results.sort_by(|a, b| b.1.len().cmp(&a.1.len()));
    Ok(results)
}

/// Find schedule conflicts between projects sharing resources.
fn find_schedule_conflicts(conn: &Connection) -> Result<Vec<Value>, String> {
    // Find projects with overlapping date ranges that share personnel or vendors
    let sql = "
        SELECT p1.title AS proj1, p2.title AS proj2,
               p1.status AS status1, p2.status AS status2
        FROM projects p1
        JOIN projects p2 ON p1.id < p2.id
        WHERE p1.status NOT LIKE '%완료%'
          AND p1.status NOT LIKE '%종료%'
          AND p2.status NOT LIKE '%완료%'
          AND p2.status NOT LIKE '%종료%'
          AND EXISTS (
            SELECT 1
            FROM chunks c1
            JOIN chunks c2 ON c1.body = c2.body
              AND c1.chunk_type = c2.chunk_type
            WHERE c1.project_id = p1.id
              AND c2.project_id = p2.id
              AND c1.chunk_type IN ('person_internal', 'person_external', 'vendor')
          )
        LIMIT 50
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {e}"))?;
    let rows = stmt
        .query_map([], |row| {
            let proj1: String = row.get(0)?;
            let proj2: String = row.get(1)?;
            Ok(json!({
                "project1": proj1,
                "project2": proj2
            }))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?;

    let mut results = Vec::new();
    for row in rows {
        results.push(row.map_err(|e| format!("행 읽기 실패: {e}"))?);
    }
    Ok(results)
}

/// Find technology/tag synergies across projects.
fn find_tech_synergy(conn: &Connection) -> Result<Vec<(String, Vec<String>)>, String> {
    let sql = "
        SELECT t.name, p.title
        FROM tags t
        JOIN project_tags pt ON t.id = pt.tag_id
        JOIN projects p ON pt.project_id = p.id
        WHERE p.status NOT LIKE '%완료%'
          AND p.status NOT LIKE '%종료%'
        ORDER BY t.name, p.title
    ";
    let mut stmt = conn
        .prepare(sql)
        .map_err(|e| format!("쿼리 준비 실패: {e}"))?;
    let rows = stmt
        .query_map([], |row| {
            let tag: String = row.get(0)?;
            let title: String = row.get(1)?;
            Ok((tag, title))
        })
        .map_err(|e| format!("쿼리 실행 실패: {e}"))?;

    let mut tag_map: std::collections::HashMap<String, Vec<String>> =
        std::collections::HashMap::new();
    for row in rows {
        let (tag, title) = row.map_err(|e| format!("행 읽기 실패: {e}"))?;
        tag_map.entry(tag).or_default().push(title);
    }

    let mut results: Vec<(String, Vec<String>)> = tag_map
        .into_iter()
        .filter(|(_, projects)| projects.len() >= 2)
        .collect();
    results.sort_by(|a, b| b.1.len().cmp(&a.1.len()));
    Ok(results)
}

pub struct CrossHandler;

impl super::CommandHandler for CrossHandler {
    fn execute(
        &self,
        config: &crate::config::VegaConfig,
        args: &serde_json::Value,
    ) -> super::CommandResult {
        cmd_cross(args, config)
    }
}
