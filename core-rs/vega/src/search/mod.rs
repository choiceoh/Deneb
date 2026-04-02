//! Vega search engine: query analysis, FTS5 search, and result fusion.
//!
//! Port of Python vega/search/router.py.

pub mod fts_search;
pub mod fusion;
pub mod query_analyzer;
pub mod semantic;

use rustc_hash::FxHashMap;

use rusqlite::Connection;
use serde::{Deserialize, Serialize};

use crate::config::VegaConfig;
use crate::db::schema::init_db;
use fts_search::sqlite_search;
use fusion::{rerank_fusion, sqlite_rows_to_unified, ProjectScore, UnifiedResult};
use query_analyzer::{analyze_query, normalize_query, QueryAnalysis, SearchRoute};

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
/// semantic search via pre-computed embeddings (SGLang/Gemini), and applies
/// fusion ranking.
pub struct SearchRouter {
    config: VegaConfig,
}

impl SearchRouter {
    pub fn new(config: VegaConfig) -> Self {
        Self { config }
    }

    /// Check if semantic search is available.
    /// Semantic is available when embeddings are provided externally (SGLang/Gemini).
    fn semantic_available(&self) -> bool {
        self.config.has_sglang()
    }

    /// Execute a full search (no pre-computed embedding).
    pub fn search(&self, query: &str) -> Result<SearchResult, Box<dyn std::error::Error>> {
        self.search_with_embedding(query, None)
    }

    /// Execute a full search with an explicit mode override.
    /// `mode_override` maps: "bm25" → Sqlite, "semantic" → Semantic, "hybrid" → Hybrid.
    /// When `None`, the route is determined by query analysis (default behavior).
    pub fn search_with_mode(
        &self,
        query: &str,
        query_embedding: Option<&[f32]>,
        mode_override: Option<&str>,
    ) -> Result<SearchResult, Box<dyn std::error::Error>> {
        let forced_route = mode_override.and_then(|m| match m {
            "bm25" => Some(SearchRoute::Sqlite),
            "semantic" => Some(SearchRoute::Semantic),
            "hybrid" => Some(SearchRoute::Hybrid),
            _ => None,
        });
        self.search_inner(query, query_embedding, forced_route)
    }

    /// Execute a full search: analyze → SQLite FTS → semantic (optional) → fusion → unified.
    /// When `query_embedding` is provided (from SGLang/Gemini), uses it for semantic search.
    pub fn search_with_embedding(
        &self,
        query: &str,
        query_embedding: Option<&[f32]>,
    ) -> Result<SearchResult, Box<dyn std::error::Error>> {
        self.search_inner(query, query_embedding, None)
    }

    /// Internal search implementation. `forced_route` overrides the query-analysis route
    /// when the caller explicitly selects bm25/semantic/hybrid.
    fn search_inner(
        &self,
        query: &str,
        query_embedding: Option<&[f32]>,
        forced_route: Option<SearchRoute>,
    ) -> Result<SearchResult, Box<dyn std::error::Error>> {
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
        let mut analysis = analyze_query(&query);

        // Apply mode override if provided.
        if let Some(route) = forced_route {
            analysis.route = route;
            analysis.reason = format!(
                "forced:{}",
                match route {
                    SearchRoute::Sqlite => "bm25",
                    SearchRoute::Semantic => "semantic",
                    SearchRoute::Hybrid => "hybrid",
                }
            );
        }

        let extracted = &analysis.extracted;

        // 2. Run SQLite search
        let conn = Connection::open(&self.config.db_path)?;
        init_db(&conn)?;
        let mut sqlite_result = sqlite_search(&conn, &query, extracted);

        // 3. Semantic search via pre-computed query embedding (SGLang/Gemini).
        let mut semantic_count = 0;
        let mut semantic_used = false;

        let should_run_semantic = match analysis.route {
            SearchRoute::Semantic => true,
            SearchRoute::Hybrid => true,
            SearchRoute::Sqlite => {
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

            // Use pre-computed vector from SGLang/Gemini.
            let sem_results = if let Some(qvec) = query_embedding {
                semantic::semantic_search_with_vec(
                    &conn,
                    qvec,
                    &semantic::SemanticConfig::default(),
                    project_filter,
                )
            } else {
                Vec::new()
            };

            if !sem_results.is_empty() {
                semantic_used = true;
                semantic_count = sem_results.len();

                for sr in sem_results {
                    if !sqlite_result
                        .chunks
                        .iter()
                        .any(|c| c.chunk_id == sr.chunk_id)
                    {
                        sqlite_result.chunks.push(sr);
                    }
                }
            }
        }

        // Note: ML reranking removed — using cosine similarity + BM25 fusion only.

        // 4. Apply fusion ranking
        let project_scores = if self.config.rerank_mode != "none" {
            rerank_fusion(&mut sqlite_result, extracted)
        } else {
            Vec::new()
        };

        // 5. Build unified results (consumes chunks to avoid cloning)
        let sqlite_count = sqlite_result.chunks.len();
        let mut unified = sqlite_rows_to_unified(sqlite_result.chunks);

        // Apply project scores to unified results
        let score_map: FxHashMap<i64, f64> = project_scores
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
                sqlite_count,
                semantic_count,
                rerank_mode: self.config.rerank_mode.clone(),
            },
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::Deserialize;
    use tempfile::TempDir;

    #[derive(Debug, Deserialize)]
    struct RegressionFixture {
        projects: Vec<FixtureProject>,
        queries: Vec<RegressionQuery>,
    }

    #[derive(Debug, Deserialize)]
    struct FixtureProject {
        name: String,
        client: String,
        status: String,
        person_internal: String,
        source_file: String,
        chunks: Vec<FixtureChunk>,
        comms: Vec<FixtureComm>,
    }

    #[derive(Debug, Deserialize)]
    struct FixtureChunk {
        section_heading: String,
        content: String,
        chunk_type: String,
    }

    #[derive(Debug, Deserialize)]
    struct FixtureComm {
        log_date: String,
        sender: String,
        subject: String,
        summary: String,
    }

    #[derive(Debug, Deserialize)]
    struct RegressionQuery {
        query: String,
        expected_project: String,
    }

    fn load_regression_fixture() -> Result<RegressionFixture, Box<dyn std::error::Error>> {
        let raw = include_str!("testdata/search_regression_fixture.json");
        Ok(serde_json::from_str(raw)?)
    }

    fn setup_test_env() -> Result<(TempDir, VegaConfig), Box<dyn std::error::Error>> {
        let dir = TempDir::new()?;
        let db_path = dir.path().join("test.db");
        let md_dir = dir.path().join("projects");
        std::fs::create_dir_all(&md_dir)?;

        let config = VegaConfig {
            db_path: db_path.clone(),
            md_dir,
            ..VegaConfig::default()
        };

        let fixture = load_regression_fixture()?;

        // Initialize DB and insert deterministic regression fixture data.
        let conn = Connection::open(&db_path)?;
        init_db(&conn)?;

        for project in &fixture.projects {
            conn.execute(
                "INSERT INTO projects (name, client, status, person_internal, source_file)
                 VALUES (?1, ?2, ?3, ?4, ?5)",
                (
                    &project.name,
                    &project.client,
                    &project.status,
                    &project.person_internal,
                    &project.source_file,
                ),
            )?;
            let project_id = conn.last_insert_rowid();

            for chunk in &project.chunks {
                conn.execute(
                    "INSERT INTO chunks (project_id, section_heading, content, chunk_type)
                     VALUES (?1, ?2, ?3, ?4)",
                    (
                        project_id,
                        &chunk.section_heading,
                        &chunk.content,
                        &chunk.chunk_type,
                    ),
                )?;
            }

            for comm in &project.comms {
                conn.execute(
                    "INSERT INTO comm_log (project_id, log_date, sender, subject, summary)
                     VALUES (?1, ?2, ?3, ?4, ?5)",
                    (
                        project_id,
                        &comm.log_date,
                        &comm.sender,
                        &comm.subject,
                        &comm.summary,
                    ),
                )?;
            }
        }

        drop(conn);
        Ok((dir, config))
    }

    #[test]
    fn test_search_router_regression_queries() -> Result<(), Box<dyn std::error::Error>> {
        let fixture = load_regression_fixture()?;
        let (_dir, config) = setup_test_env()?;
        let router = SearchRouter::new(config);

        for case in &fixture.queries {
            let result = router.search(&case.query)?;
            assert!(
                !result.unified.is_empty(),
                "expected results for query: {}",
                case.query
            );

            let top = &result.unified[0];
            assert_eq!(
                top.project_name, case.expected_project,
                "top project mismatch for query: {}",
                case.query
            );
        }

        Ok(())
    }

    #[test]
    fn test_search_router_empty() -> Result<(), Box<dyn std::error::Error>> {
        let (_dir, config) = setup_test_env()?;
        let router = SearchRouter::new(config);

        let result = router.search("")?;
        assert!(result.unified.is_empty());
        Ok(())
    }
}
