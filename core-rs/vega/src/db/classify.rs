//! Section classification and tag extraction.
//!
//! Port of Python vega/db/classify.py — solar EPC domain taxonomy.

use std::collections::HashSet;

use regex::Regex;
use rustc_hash::FxHashMap;

/// Classify a section by heading and content keywords.
/// Returns one of: status, `next_action`, history, `comm_log`, technical, issue,
/// schedule, financial, permit, summary, contract, attachment, other.
pub fn classify_section(heading: &str, content: &str) -> &'static str {
    let h = heading.to_lowercase();
    let c = content.to_lowercase();

    // Heading-based classification
    let heading_rules: &[(&[&str], &str)] = &[
        (
            &["현재 상황", "현재상황", "개요", "프로젝트 개요"],
            "status",
        ),
        (&["다음 예상", "액션"], "next_action"),
        (&["이력"], "history"),
        (&["로그 20"], "comm_log"),
        (&["기술", "사양", "사업 기본"], "technical"),
        (&["이슈", "리스크", "문제", "화재"], "issue"),
        (&["일정", "마일스톤", "공정"], "schedule"),
        (
            &["경제성", "투자비", "공사비", "운영비", "재무"],
            "financial",
        ),
        (&["인허가", "규제"], "permit"),
        (&["결론", "요약", "종합"], "summary"),
        (&["관련 메일", "메일"], "comm_log"),
        (&["첨부", "자료"], "attachment"),
    ];

    for (keywords, label) in heading_rules {
        if keywords.iter().any(|k| h.contains(k)) {
            return label;
        }
    }

    // Content-based fallback
    if ["견적", "계약서", "공사도급"].iter().any(|k| c.contains(k)) {
        return "contract";
    }
    if ["화재", "소손", "클레임"].iter().any(|k| c.contains(k)) {
        return "issue";
    }

    "other"
}

/// Extract domain-specific tags from metadata and sections.
/// Returns a set of tags like "고객:한국전력", "기술:EPC", "유형:지붕태양광", etc.
pub fn extract_tags(
    meta: &FxHashMap<String, String>,
    sections: &[(String, String)], // (heading, body)
) -> HashSet<String> {
    let mut tags = HashSet::new();

    // Meta-based tags
    if let Some(client) = meta.get("client") {
        if !client.is_empty() {
            tags.insert(format!("고객:{client}"));
        }
    }
    if let Some(person) = meta.get("person_internal") {
        #[allow(clippy::expect_used)]
        let splitter = Regex::new(r"[,/·]").expect("valid regex");
        for p in splitter.split(person) {
            let p = p.trim();
            if !p.is_empty() {
                tags.insert(format!("담당:{p}"));
            }
        }
    }
    if let Some(status) = meta.get("status") {
        if !status.is_empty() {
            tags.insert(format!("상태:{status}"));
        }
    }

    // Aggregate all text for keyword matching
    let mut all_text = sections
        .iter()
        .map(|(_, body)| body.as_str())
        .collect::<Vec<_>>()
        .join(" ")
        .to_lowercase();
    all_text.push(' ');
    all_text.push_str(&meta.get("name").cloned().unwrap_or_default().to_lowercase());
    all_text.push(' ');
    all_text.push_str(
        &meta
            .get("biz_type")
            .cloned()
            .unwrap_or_default()
            .to_lowercase(),
    );

    // Technical fields with _ prefix → tags
    for (key, val) in meta {
        if key.starts_with('_') && !val.is_empty() {
            let tag_name = key.trim_start_matches('_');
            tags.insert(format!("기술:{tag_name}"));
        }
    }

    // Solar EPC domain keywords
    let tech_kw: &[(&str, &[&str])] = &[
        ("EPC", &["epc", "시공"]),
        ("O&M", &["o&m", "운영관리", "유지관리", "유지보수"]),
        ("PPA", &["ppa", "직접전력거래", "전력거래"]),
        ("설비리스", &["설비리스", "리스사업", "임대차"]),
        ("ESS", &["ess", "bess", "에너지저장"]),
        ("해저케이블", &["해저케이블", "submarine cable", "154kv"]),
        (
            "모듈",
            &[
                "모듈",
                "module",
                "진코",
                "jinko",
                "ja solar",
                "트리나",
                "한화",
            ],
        ),
        ("인버터", &["인버터", "inverter", "화웨이", "huawei", "pcs"]),
        ("구조검토", &["구조검토", "구조계산", "구조물"]),
        ("TPO방수", &["tpo", "방수", "현대l&c"]),
        (
            "환경공단",
            &["환경공단", "탄소중립", "감축설비", "지원사업"],
        ),
        ("PF금융", &["pf", "팩토링", "대출", "금융조건", "펀드"]),
        ("CU헷징", &["헷징", "hedging", "lme"]),
        ("MC4화재", &["mc4", "커넥터 화재", "소손"]),
        ("해상풍력", &["해상풍력", "풍력", "풍황"]),
        ("수상태양광", &["수상태양광", "수상"]),
        ("접속단", &["접속단", "계통연계", "kepco", "한전"]),
        ("REC", &["rec", "rec 가중치", "rps"]),
        ("SMP", &["smp"]),
    ];
    for (tag, keywords) in tech_kw {
        if keywords.iter().any(|kw| all_text.contains(kw)) {
            tags.insert(format!("기술:{tag}"));
        }
    }

    // Project type keywords
    let type_kw: &[(&str, &[&str])] = &[
        ("지붕태양광", &["지붕", "루프탑", "rooftop"]),
        ("주차장태양광", &["주차장", "캐노피"]),
        ("수상태양광", &["수상", "석문호"]),
        ("지상태양광", &["토지", "지상"]),
        ("해상풍력", &["해상풍력"]),
        ("BESS", &["bess", "ess 발전"]),
    ];
    for (tag, keywords) in type_kw {
        if keywords.iter().any(|kw| all_text.contains(kw)) {
            tags.insert(format!("유형:{tag}"));
        }
    }

    // Hyundai group affiliation
    let hyundai = ["현대", "기아", "모비스", "글로비스", "위아"];
    if hyundai.iter().any(|kw| all_text.contains(kw)) {
        tags.insert("그룹:현대차".into());
    }

    tags
}
