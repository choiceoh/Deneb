//! Vega search engine: query analysis, FTS5 search, and result fusion.
//!
//! Port of Python vega/search/router.py.

pub mod query_analyzer;
pub mod fts_search;
pub mod fusion;
pub mod semantic;

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

/// Main search router. Analyzes queries, runs SQLite search, optionally runs
/// semantic search via deneb-ml, and applies fusion ranking.
pub struct SearchRouter {
    config: VegaConfig,
    #[cfg(feature = "ml")]
    ml_manager: Option<deneb_ml::ModelManager>,
}

impl SearchRouter {
    pub fn new(config: VegaConfig) -> Self {
        #[cfg(feature = "ml")]
        let ml_manager = build_ml_manager(&config);

        Self {
            config,
            #[cfg(feature = "ml")]
            ml_manager,
        }
    }

    /// Check if semantic search is available (ML models configured + feature enabled).
    fn semantic_available(&self) -> bool {
        #[cfg(feature = "ml")]
        {
            self.ml_manager.is_some()
        }
        #[cfg(not(feature = "ml"))]
        {
            false
        }
    }

    /// Execute a full search: analyze → SQLite FTS → semantic (optional) → fusion → unified.
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
                    semantic_available: self.semantic_available(),
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

        // 3. Semantic search (when ML is available and route permits)
        let mut semantic_count = 0;
        let mut semantic_used = false;

        #[cfg(feature = "ml")]
        if let Some(ref mgr) = self.ml_manager {
            let should_run_semantic = match analysis.route {
                SearchRoute::Semantic => true,
                SearchRoute::Hybrid => true,
                SearchRoute::Sqlite => {
                    // Fallback: run semantic if SQLite returned too few results
                    query_analyzer::has_semantic_pattern(&query)
                        || (!extracted.keywords.is_empty() && sqlite_result.chunks.len() < 5)
                }
            };

            if should_run_semantic {
                let project_filter = if !sqlite_result.project_ids.is_empty()
                    && analysis.route != SearchRoute::Semantic
                {
                    Some(sqlite_result.project_ids.as_slice())
                } else {
                    None
                };

                let sem_results = semantic::semantic_search(
                    &conn,
                    &query,
                    &semantic::SemanticConfig::default(),
                    project_filter,
                    mgr,
                );

                if !sem_results.is_empty() {
                    semantic_used = true;
                    semantic_count = sem_results.len();

                    // Convert semantic results to unified format and merge
                    for sr in &sem_results {
                        // Avoid duplicating chunks already in SQLite results
                        if !sqlite_result.chunks.iter().any(|c| c.chunk_id == sr.chunk_id) {
                            sqlite_result.chunks.push(fts_search::ChunkRow {
                                chunk_id: sr.chunk_id,
                                project_id: sr.project_id,
                                name: sr.project_name.clone(),
                                client: sr.client.clone(),
                                status: sr.status.clone(),
                                person_internal: sr.person_internal.clone(),
                                capacity: String::new(),
                                section_heading: sr.section_heading.clone(),
                                content: sr.content.clone(),
                                chunk_type: sr.chunk_type.clone(),
                                entry_date: sr.entry_date.clone(),
                            });
                        }
                    }
                }
            }

            // 3b. ML reranking (when rerank_mode is "full")
            if self.config.rerank_mode == "full" && !sqlite_result.chunks.is_empty() {
                let reranked = semantic::rerank_results(&query, &sqlite_result.chunks, 30, mgr);
                if !reranked.is_empty() {
                    // Apply reranker scores as boost to fusion
                    // reranked is [(original_index, score)]
                    for (idx, ml_score) in &reranked {
                        if let Some(chunk) = sqlite_result.chunks.get(*idx) {
                            // We'll apply these as metadata for fusion to pick up
                            log::debug!(
                                "Rerank: chunk {} score {:.3}",
                                chunk.chunk_id,
                                ml_score
                            );
                        }
                    }
                }
            }
        }

        // 4. Apply fusion ranking
        let project_scores = if self.config.rerank_mode != "none" {
            rerank_fusion(&mut sqlite_result, extracted)
        } else {
            Vec::new()
        };

        // 5. Build unified results
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
                semantic_available: self.semantic_available(),
                semantic_used,
                sqlite_count: sqlite_result.chunks.len(),
                semantic_count,
                rerank_mode: self.config.rerank_mode.clone(),
            },
        })
    }
}

/// Build ML ModelManager from VegaConfig (feature-gated).
#[cfg(feature = "ml")]
fn build_ml_manager(config: &VegaConfig) -> Option<deneb_ml::ModelManager> {
    if config.inference_backend != "local" {
        return None;
    }

    let mut configs = Vec::new();

    if let Some(ref path) = config.model_embedder {
        configs.push(deneb_ml::ModelConfig::embedder(
            path.clone(),
            config.model_unload_ttl,
        ));
    }
    if let Some(ref path) = config.model_reranker {
        configs.push(deneb_ml::ModelConfig::reranker(
            path.clone(),
            config.model_unload_ttl,
        ));
    }

    if configs.is_empty() {
        None
    } else {
        Some(deneb_ml::ModelManager::new(configs))
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
