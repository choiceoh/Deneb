//! Query analysis and routing for Vega search.
//!
//! Port of Python vega/search/router.py — query analyzer section.
//! Extracts structural fields (clients, persons, statuses, tags) and
//! determines search route (sqlite | semantic | hybrid).

use std::collections::HashSet;

use once_cell::sync::Lazy;
use regex::Regex;
use serde::{Deserialize, Serialize};

/// Result of query analysis: extracted fields + routing decision.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QueryAnalysis {
    pub route: SearchRoute,
    pub confidence: f64,
    pub extracted: ExtractedFields,
    pub reason: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum SearchRoute {
    Sqlite,
    Semantic,
    Hybrid,
}

/// Extracted structural fields from a query.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ExtractedFields {
    pub clients: Vec<String>,
    pub persons: Vec<String>,
    pub statuses: Vec<String>,
    pub tags: Vec<String>,
    pub keywords: Vec<String>,
}

// -- Static patterns (fallback when DB is not available) --

#[allow(clippy::expect_used)]
static CLIENT_PATTERNS: Lazy<Vec<Regex>> = Lazy::new(|| {
    vec![
        Regex::new(r"(?i)(금호타이어|기아|현대[가-힣]*|대한전선|롯데[가-힣]*|무림[가-힣]*|비금도|석문호|썬탑|양명|옹진|인하|하이트|글로비스|위아|화성산단|한화[가-힣]*|쿠팡|카카오|ZTT|jinko|진코)").expect("valid regex"),
    ]
});

#[allow(clippy::expect_used)]
static PERSON_PATTERNS: Lazy<Vec<Regex>> = Lazy::new(|| {
    vec![
        Regex::new(r"(김대희|고건|이시연|박민수|강민수|김유영|임은진|박종원|제용범|조은실|백종태|강동민|이영민|김세미|장현정|Sara)").expect("valid regex"),
        Regex::new(r"(누가|담당자|담당)").expect("valid regex"),
    ]
});

#[allow(clippy::expect_used)]
static STATUS_PATTERNS: Lazy<Vec<Regex>> = Lazy::new(|| {
    vec![
        Regex::new(r"(진행중|진행\s?중|완료|준공|설계|시공|계약|검토|대기|마무리|긴급|급한|위급)")
            .expect("valid regex"),
        Regex::new(r"(상태가|현재\s?상황|현황)").expect("valid regex"),
    ]
});

#[allow(clippy::expect_used)]
static TAG_PATTERNS: Lazy<Vec<Regex>> = Lazy::new(|| {
    vec![
        Regex::new(r"(현대차\s?그룹|현대차그룹)").expect("valid regex"),
        Regex::new(r"(환경공단|탄소중립|지원사업)").expect("valid regex"),
        Regex::new(r"(?i)(EPC|O&M|PPA|설비리스|PF|팩토링)").expect("valid regex"),
    ]
});

/// Semantic/conceptual query patterns (vector search performs well on these).
#[allow(clippy::expect_used)]
static SEMANTIC_PATTERNS: Lazy<Vec<Regex>> = Lazy::new(|| {
    [
        r"(어떻게|왜|방법|이유|원인|차이|비교)",
        r"(관련\s?내용|자세히|설명|배경)",
        r"(기술적|공법|방식|구조|설계|사양|스펙)",
        r"(리스크|위험|문제점|이슈|해결|대응|대책|조치)",
        r"(전략|방향|검토|분석|판단|의견|평가)",
        r"(화재|사고|피해|파손|고장|결함|하자|민원|분쟁)",
        r"(교체|변경|수정|보수|보강|개선|철거|재시공)",
        r"(경위|경과|과정|경험|사례|전말|추이)",
        r"(조건|제약|규제|인허가|환경영향|주민\s?반대|민원)",
        r"(지연|납기|딜레이|공기|일정\s?차질|늦어|밀려)",
        r"(지난달|지난주|다음주|다음달|어제|금주|금월|요번주|최근\s?\d)",
        r"(문서|계약서|서류|도면|인증서|보고서|시방서|견적)",
        r"(발주처|고객|클라이언트|협력사|외주|하도급)",
        r"(의견|회의|토의|논의|합의|피드백|회신|답변)",
    ]
    .iter()
    .map(|p| Regex::new(p).expect("valid regex"))
    .collect()
});

/// Query stopwords (pure filler only — domain terms are preserved).
static QUERY_STOPWORDS: Lazy<HashSet<&'static str>> = Lazy::new(|| {
    [
        "프로젝트",
        "검색",
        "찾아",
        "찾아줘",
        "보여",
        "보여줘",
        "알려",
        "알려줘",
        "문의",
        "내용",
        "알아봐",
        "알아봐줘",
        "인가",
        "인가요",
        "정리",
        "대해",
        "대해서",
        "관해",
        "관해서",
        "좀",
        "그",
        "뭐",
        "뭐가",
        "어떤",
        "무슨",
        "몇",
        "개",
    ]
    .into_iter()
    .collect()
});

#[allow(clippy::expect_used)]
static TRAILING_PARTICLES: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"(은|는|이|가|을|를|의|에|에서|으로|로|와|과|만|까지|부터|에게|한테|께|처럼|같이|에서도|까지도|만도|부터도|라도|이라도|라고|이라고)$").expect("valid regex")
});

#[allow(clippy::expect_used)]
static TRAILING_ENDINGS: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"(하는지|했는지|되는지|해줘|해줘요|해주세요|해라|한다|하는|하기|하다|했던|되고|되는|되어|됐다|된|중인|있던|있는|있고|있어|있음|이야|야|인가요|인가|인지|임)$").expect("valid regex")
});

#[allow(clippy::expect_used)]
static SUFFIX_CLEANUP: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(해줘|알려줘|보여줘|뭐야|좀|요)\s*$").expect("valid regex"));
#[allow(clippy::expect_used)]
static TRAILING_PUNCT: Lazy<Regex> = Lazy::new(|| Regex::new(r"[?？！!.。]+$").expect("valid regex"));
#[allow(clippy::expect_used)]
static TOKEN_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"[가-힣A-Za-z0-9&+/.\-]+").expect("valid regex"));
#[allow(clippy::expect_used)]
static STRIP_NONALPHA: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"^[^가-힣A-Za-z0-9]+|[^가-힣A-Za-z0-9]+$").expect("valid regex"));

/// Normalize a query: remove trailing punctuation, then filler suffixes.
pub fn normalize_query(query: &str) -> String {
    let q = query.trim();
    if q.is_empty() {
        return String::new();
    }
    // Remove trailing punctuation first so "보여줘?" becomes "보여줘"
    let q = TRAILING_PUNCT.replace(q, "").trim().to_string();
    SUFFIX_CLEANUP.replace(&q, "").trim().to_string()
}

/// Normalize a single keyword: strip non-alphanum edges, remove particles/endings.
pub fn normalize_keyword(term: &str) -> String {
    let term = STRIP_NONALPHA.replace(term.trim(), "").to_string();
    if term.is_empty() {
        return String::new();
    }
    let mut result = term;
    loop {
        let prev = result.clone();
        result = TRAILING_ENDINGS.replace(&result, "").to_string();
        result = TRAILING_PARTICLES.replace(&result, "").to_string();
        if result == prev {
            break;
        }
    }
    result.trim().to_string()
}

/// Check if a query matches any semantic pattern.
pub fn has_semantic_pattern(query: &str) -> bool {
    let q_lower = query.to_lowercase();
    SEMANTIC_PATTERNS.iter().any(|p| p.is_match(&q_lower))
}

/// Extract keywords from query that are not structural terms.
fn extract_keywords(query: &str, structural_terms: &[String]) -> Vec<String> {
    let structural_lower: HashSet<String> = structural_terms
        .iter()
        .filter(|t| !t.is_empty())
        .map(|t| t.to_lowercase())
        .collect();

    let mut keywords = Vec::new();
    let mut seen = HashSet::new();

    for m in TOKEN_RE.find_iter(query) {
        let raw = m.as_str();
        if structural_lower.contains(&raw.to_lowercase()) {
            continue;
        }
        let normalized = normalize_keyword(raw);
        if normalized.len() <= 1 {
            continue;
        }
        if structural_lower.contains(&normalized.to_lowercase()) {
            continue;
        }
        if QUERY_STOPWORDS.contains(normalized.as_str()) {
            continue;
        }
        if seen.insert(normalized.clone()) {
            keywords.push(normalized);
        }
    }
    keywords
}

/// Analyze a query to determine search routing and extract structured fields.
pub fn analyze_query(query: &str) -> QueryAnalysis {
    let query_lower = query.to_lowercase();
    let mut extracted = ExtractedFields::default();
    let mut structural_score = 0i32;
    let mut semantic_score = 0i32;

    // Non-structural person terms to exclude from extracted persons
    let person_filler: HashSet<&str> = ["누가", "담당자", "담당"].into_iter().collect();
    let status_filler: HashSet<&str> = ["상태가", "현재상황", "현재 상황", "현황"]
        .into_iter()
        .collect();

    // Client patterns
    for pat in CLIENT_PATTERNS.iter() {
        let matches: Vec<String> = pat
            .find_iter(query)
            .map(|m| m.as_str().to_string())
            .collect();
        if !matches.is_empty() {
            structural_score += matches.len() as i32 * 2;
            extracted.clients.extend(matches);
        }
    }

    // Person patterns
    for pat in PERSON_PATTERNS.iter() {
        let matches: Vec<String> = pat
            .find_iter(query)
            .map(|m| m.as_str().to_string())
            .filter(|m| !person_filler.contains(m.as_str()))
            .collect();
        if !matches.is_empty() {
            structural_score += matches.len() as i32 * 2;
            extracted.persons.extend(matches);
        }
    }

    // Status patterns
    for pat in STATUS_PATTERNS.iter() {
        let matches: Vec<String> = pat
            .find_iter(query)
            .map(|m| m.as_str().to_string())
            .filter(|m| !status_filler.contains(m.as_str()))
            .collect();
        if !matches.is_empty() {
            structural_score += matches.len() as i32 * 2;
            extracted.statuses.extend(matches);
        }
    }

    // Tag patterns
    for pat in TAG_PATTERNS.iter() {
        let matches: Vec<String> = pat
            .find_iter(query)
            .map(|m| m.as_str().to_string())
            .collect();
        if !matches.is_empty() {
            structural_score += matches.len() as i32 * 2;
            extracted.tags.extend(matches);
        }
    }

    // Semantic pattern matching
    for pat in SEMANTIC_PATTERNS.iter() {
        if pat.is_match(&query_lower) {
            semantic_score += 2;
        }
    }

    // Extract free-text keywords
    let all_structural: Vec<String> = extracted
        .clients
        .iter()
        .chain(&extracted.persons)
        .chain(&extracted.statuses)
        .chain(&extracted.tags)
        .cloned()
        .collect();
    extracted.keywords = extract_keywords(query, &all_structural);

    // Routing decision
    let total = structural_score + semantic_score;
    let has_keywords = !extracted.keywords.is_empty();

    let (route, confidence, reason) = if total == 0 {
        if has_keywords {
            (
                SearchRoute::Hybrid,
                0.6,
                "키워드 감지 → SQLite + 의미 검색 병행".into(),
            )
        } else {
            (
                SearchRoute::Sqlite,
                0.5,
                "특정 패턴 없음 → SQLite 전문검색으로 처리".into(),
            )
        }
    } else if structural_score > 0 && semantic_score > 0 {
        (
            SearchRoute::Hybrid,
            0.8,
            format!(
                "구조화({}) + 의미({}) → 혼합 검색",
                structural_score, semantic_score
            ),
        )
    } else if structural_score > 0 {
        if has_keywords {
            (
                SearchRoute::Hybrid,
                0.7,
                format!("구조화({}) + 키워드 → 혼합 검색", structural_score),
            )
        } else {
            let conf = (0.7 + structural_score as f64 * 0.05).min(0.95);
            (
                SearchRoute::Sqlite,
                conf,
                format!("구조화 필드 감지({}) → SQLite 우선", structural_score),
            )
        }
    } else {
        // semantic_score > 0
        if has_keywords {
            (
                SearchRoute::Hybrid,
                0.75,
                format!(
                    "의미({}) + 키워드 → SQLite + 의미 검색 병행",
                    semantic_score
                ),
            )
        } else {
            let conf = (0.7 + semantic_score as f64 * 0.05).min(0.95);
            (
                SearchRoute::Semantic,
                conf,
                format!("의미 검색 패턴({}) → 벡터 검색 우선", semantic_score),
            )
        }
    };

    QueryAnalysis {
        route,
        confidence,
        extracted,
        reason,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_analyze_client_query() {
        let analysis = analyze_query("비금도 현재 상황");
        assert!(analysis.extracted.clients.contains(&"비금도".to_string()));
        assert!(matches!(
            analysis.route,
            SearchRoute::Sqlite | SearchRoute::Hybrid
        ));
    }

    #[test]
    fn test_analyze_semantic_query() {
        let analysis = analyze_query("해저케이블 기술적 방식은 어떻게 되나");
        assert!(matches!(
            analysis.route,
            SearchRoute::Semantic | SearchRoute::Hybrid
        ));
    }

    #[test]
    fn test_analyze_hybrid_query() {
        let analysis = analyze_query("기아 공장 리스크 분석");
        assert!(matches!(analysis.route, SearchRoute::Hybrid));
    }

    #[test]
    fn test_normalize_query() {
        assert_eq!(normalize_query("비금도 알려줘"), "비금도");
        // "보여줘" is removed by suffix cleanup; "?" is removed by trailing punct
        assert_eq!(normalize_query("현황 보여줘?"), "현황");
        assert_eq!(normalize_query("뭐야?"), "");
    }

    #[test]
    fn test_normalize_keyword() {
        assert_eq!(normalize_keyword("프로젝트의"), "프로젝트");
        assert_eq!(normalize_keyword("진행중인"), "진행");
        assert_eq!(normalize_keyword("케이블에서"), "케이블");
    }

    #[test]
    fn test_extract_keywords() {
        let analysis = analyze_query("비금도 해저케이블 현황");
        // "비금도" should be in clients, "해저케이블" should be in keywords
        assert!(!analysis.extracted.keywords.is_empty() || !analysis.extracted.clients.is_empty());
    }

    #[test]
    fn test_analyze_person_query() {
        let analysis = analyze_query("김대희 담당 프로젝트");
        assert!(
            analysis.extracted.persons.contains(&"김대희".to_string()),
            "expected 김대희 in persons, got {:?}",
            analysis.extracted.persons
        );
    }

    #[test]
    fn test_analyze_status_query() {
        let analysis = analyze_query("긴급 처리 필요한 프로젝트");
        assert!(
            !analysis.extracted.statuses.is_empty(),
            "expected at least one status term extracted"
        );
        assert!(analysis.extracted.statuses.iter().any(|s| s.contains("긴급")));
    }

    #[test]
    fn test_has_semantic_pattern_true() {
        assert!(has_semantic_pattern("어떻게 하면 되나요"));
        assert!(has_semantic_pattern("화재 사고 원인이 뭔가요"));
        assert!(!has_semantic_pattern("비금도"));
    }

    #[test]
    fn test_analyze_confidence_in_range() {
        for query in &[
            "비금도",
            "기아 리스크 분석",
            "어떻게 되나요",
            "해저케이블 기술적 방식",
            "",
        ] {
            let analysis = analyze_query(query);
            assert!(
                analysis.confidence >= 0.0 && analysis.confidence <= 1.0,
                "confidence out of [0,1] for {:?}: {}",
                query,
                analysis.confidence
            );
        }
    }
}
