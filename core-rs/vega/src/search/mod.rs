//! Vega search engine: query analysis, FTS5 search, and result fusion.
//!
//! Port of Python vega/search/router.py.

pub mod query_analyzer;
pub mod fts_search;
pub mod fusion;

use std::collections::HashMap;

use rusqlite::Connection;
use serde::{Deserialize, Serialize};

use crate::config::VegaConfig;
use crate::db::schema::init_db;
use query_analyzer::{analyze_query, normalize_query, QueryAnalysis, SearchRoute};
use fts_search::{sqlite_search, SqliteSearchResult};
use fusion::{rerank_fusion, sqlite_rows_to_unified, ProjectScore, UnifiedResult};

/// Full search result returned by SearchRouter.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchResult {
    pub query: String,
    pub analysis: QueryAnalysis,
    pub unified: Vec<UnifiedResult>,
    pub comms: Vec<fts_search::CommRow>,
    pub project_scores: Vec<ProjectScore>,
    pub search_meta: SearchMeta,
}

/// Metadata about the search execution.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchMeta {
    pub route: String,
    pub semantic_available: bool,
    pub semantic_used: bool,
    pub sqlite_count: usize,
    pub semantic_count: usize,
    pub rerank_mode: String,
}

/// Main search router. Analyzes queries, runs SQLite search, and applies fusion ranking.
pub struct SearchRouter {
    config: VegaConfig,
}

impl SearchRouter {
    pub fn new(config: VegaConfig) -> Self {
        Self { config }
    }

    /// Execute a full search: analyze → SQLite FTS → fusion → unified results.
    pub fn search(&self, query: &str) -> Result<SearchResult, Box<dyn std::error::Error>> {
        let query = normalize_query(query);
        if query.is_empty() {
            return Ok(SearchResult {
                query: String::new(),
                analysis: QueryAnalysis {
                    route: SearchRoute::Sqlite,
                    confidence: 0.0,
                    extracted: Default::default(),
                    reason: "empty".into(),
                },
                unified: Vec::new(),
                comms: Vec::new(),
                project_scores: Vec::new(),
                search_meta: SearchMeta {
                    route: "sqlite".into(),
                    semantic_available: false,
                    semantic_used: false,
                    sqlite_count: 0,
                    semantic_count: 0,
                    rerank_mode: self.config.rerank_mode.clone(),
                },
            });
        }

        // 1. Analyze query
        let analysis = analyze_query(&query);
        let extracted = &analysis.extracted;

        // 2. Run SQLite search
        let conn = Connection::open(&self.config.db_path)?;
        init_db(&conn)?;
        let mut sqlite_result = sqlite_search(&conn, &query, extracted);

        // 3. Apply fusion ranking
        let project_scores = if self.config.rerank_mode != "none" {
            rerank_fusion(&mut sqlite_result, extracted)
        } else {
            Vec::new()
        };

        // 4. Build unified results
        let mut unified = sqlite_rows_to_unified(&sqlite_result.chunks);

        // Apply project scores to unified results
        let score_map: HashMap<i64, f64> = project_scores
            .iter()
            .map(|s| (s.project_id, s.score))
            .collect();
        for item in &mut unified {
            if item.source == "sqlite" {
                if let Some(&s) = score_map.get(&item.project_id) {
                    item.score = s;
                }
            }
        }

        let route_str = match analysis.route {
            SearchRoute::Sqlite => "sqlite",
            SearchRoute::Semantic => "semantic",
            SearchRoute::Hybrid => "hybrid",
        };

        Ok(SearchResult {
            query: query.clone(),
            analysis,
            comms: sqlite_result.comms,
            unified,
            project_scores,
            search_meta: SearchMeta {
                route: route_str.into(),
                semantic_available: false, // Phase 1 ML integration will enable this
                semantic_used: false,
                sqlite_count: sqlite_result.chunks.len(),
                semantic_count: 0,
                rerank_mode: self.config.rerank_mode.clone(),
            },
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use tempfile::TempDir;

    fn setup_test_env() -> (TempDir, VegaConfig) {
        let dir = TempDir::new().unwrap();
        let db_path = dir.path().join("test.db");
        let md_dir = dir.path().join("projects");
        std::fs::create_dir_all(&md_dir).unwrap();

        let config = VegaConfig {
            db_path: db_path.clone(),
            md_dir,
            ..VegaConfig::default()
        };

        // Initialize DB and insert test data
        let conn = Connection::open(&db_path).unwrap();
        init_db(&conn).unwrap();

        conn.execute(
            "INSERT INTO projects (name, client, status, person_internal, source_file)
             VALUES ('비금도 해상태양광', '한국전력', '진행중', '김대희', 'bigeum.md')",
            [],
        )
        .unwrap();

        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '현재 상황', '해저케이블 154kV 설치 진행중. EPC 시공 방식.', 'status')",
            [],
        )
        .unwrap();
        conn.execute(
            "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
             VALUES (1, '기술 사양', '모듈: 진코 600W, 인버터: 화웨이', 'technical')",
            [],
        )
        .unwrap();
        conn.execute(
            "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
             VALUES (1, '2025-03-01', '김대희', '설계 검토 완료', 'KEPCO 계통연계 승인 대기중')",
            [],
        )
        .unwrap();

        drop(conn);
        (dir, config)
    }

    #[test]
    fn test_search_router_basic() {
        let (_dir, config) = setup_test_env();
        let router = SearchRouter::new(config);

        let result = router.search("비금도").unwrap();
        assert!(!result.unified.is_empty(), "Should find results for 비금도");
    }

    #[test]
    fn test_search_router_keyword() {
        let (_dir, config) = setup_test_env();
        let router = SearchRouter::new(config);

        let result = router.search("해저케이블").unwrap();
        assert!(!result.unified.is_empty(), "Should find 해저케이블 results");
    }

    #[test]
    fn test_search_router_empty() {
        let (_dir, config) = setup_test_env();
        let router = SearchRouter::new(config);

        let result = router.search("").unwrap();
        assert!(result.unified.is_empty());
    }
}
