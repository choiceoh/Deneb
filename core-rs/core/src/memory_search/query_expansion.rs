//! Query expansion and keyword extraction for memory search.

use once_cell::sync::Lazy;
use regex::Regex;

use super::stop_words;
use super::types::ExpandedQuery;

// ---------------------------------------------------------------------------
// Stop word sets — built once from the const arrays in `stop_words`
// ---------------------------------------------------------------------------

fn make_set(words: &[&'static str]) -> std::collections::HashSet<&'static str> {
    words.iter().copied().collect()
}

static STOP_WORDS_EN: Lazy<std::collections::HashSet<&'static str>> =
    Lazy::new(|| make_set(stop_words::EN));
static STOP_WORDS_ES: Lazy<std::collections::HashSet<&'static str>> =
    Lazy::new(|| make_set(stop_words::ES));
static STOP_WORDS_PT: Lazy<std::collections::HashSet<&'static str>> =
    Lazy::new(|| make_set(stop_words::PT));
static STOP_WORDS_AR: Lazy<std::collections::HashSet<&'static str>> =
    Lazy::new(|| make_set(stop_words::AR));
static STOP_WORDS_KO: Lazy<std::collections::HashSet<&'static str>> =
    Lazy::new(|| make_set(stop_words::KO));
static STOP_WORDS_JA: Lazy<std::collections::HashSet<&'static str>> =
    Lazy::new(|| make_set(stop_words::JA));
static STOP_WORDS_ZH: Lazy<std::collections::HashSet<&'static str>> =
    Lazy::new(|| make_set(stop_words::ZH));

/// Check if a token is a stop word in any supported language.
pub fn is_query_stop_word_token(token: &str) -> bool {
    STOP_WORDS_EN.contains(token)
        || STOP_WORDS_ES.contains(token)
        || STOP_WORDS_PT.contains(token)
        || STOP_WORDS_AR.contains(token)
        || STOP_WORDS_ZH.contains(token)
        || STOP_WORDS_KO.contains(token)
        || STOP_WORDS_JA.contains(token)
}

// ---------------------------------------------------------------------------
// Tokenization
// ---------------------------------------------------------------------------

#[allow(clippy::expect_used)]
static SPLIT_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"[\s\p{P}]+").expect("valid regex"));
#[allow(clippy::expect_used)]
static JP_PART_RE: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"[a-z0-9_]+|[\x{30A0}-\x{30FF}ー]+|[\x{4E00}-\x{9FFF}]+|[\x{3040}-\x{309F}]{2,}")
        .expect("valid regex")
});
#[allow(clippy::expect_used)]
static PUNCT_SYMBOL_RE: Lazy<Regex> = Lazy::new(|| Regex::new(r"^[\p{P}\p{S}]+$").expect("valid regex"));

#[inline]
fn contains_hiragana_katakana(s: &str) -> bool {
    s.chars().any(|c| ('\u{3040}'..='\u{30FF}').contains(&c))
}

#[inline]
fn contains_cjk(s: &str) -> bool {
    s.chars().any(is_cjk_char)
}

#[inline]
fn contains_hangul(s: &str) -> bool {
    s.chars()
        .any(|c| ('\u{AC00}'..='\u{D7AF}').contains(&c) || ('\u{3131}'..='\u{3163}').contains(&c))
}

#[inline]
fn is_pure_ascii_alpha(s: &str) -> bool {
    !s.is_empty() && s.bytes().all(|b| b.is_ascii_alphabetic())
}

#[inline]
fn is_pure_digits(s: &str) -> bool {
    !s.is_empty() && s.bytes().all(|b| b.is_ascii_digit())
}

#[inline]
fn contains_hangul_syllable(s: &str) -> bool {
    s.chars().any(|c| ('\u{AC00}'..='\u{D7AF}').contains(&c))
}

#[inline]
fn is_ascii_stem(s: &str) -> bool {
    !s.is_empty() && s.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'_')
}

fn strip_korean_trailing_particle(token: &str) -> Option<String> {
    for &particle in stop_words::KO_TRAILING_PARTICLES {
        if token.len() > particle.len() && token.ends_with(particle) {
            // `ends_with` on &str guarantees the split point is a valid char boundary,
            // so the slice is always safe. Double-check defensively.
            let split_at = token.len() - particle.len();
            if !token.is_char_boundary(split_at) {
                continue;
            }
            return Some(token[..split_at].to_string());
        }
    }
    None
}

fn is_useful_korean_stem(stem: &str) -> bool {
    if contains_hangul_syllable(stem) {
        return stem.chars().count() >= 2;
    }
    is_ascii_stem(stem)
}

fn is_cjk_char(c: char) -> bool {
    ('\u{4E00}'..='\u{9FFF}').contains(&c)
}

fn is_valid_keyword(token: &str) -> bool {
    if token.is_empty() {
        return false;
    }
    // Skip very short English words (likely stop words or fragments)
    if is_pure_ascii_alpha(token) && token.len() < 3 {
        return false;
    }
    // Skip pure numbers
    if is_pure_digits(token) {
        return false;
    }
    // Skip tokens that are all punctuation/symbols
    if PUNCT_SYMBOL_RE.is_match(token) {
        return false;
    }
    true
}

fn tokenize(text: &str) -> Vec<String> {
    let normalized = text.to_lowercase();
    let normalized = normalized.trim();
    let mut tokens = Vec::new();

    let segments: Vec<&str> = SPLIT_RE
        .split(normalized)
        .filter(|s| !s.is_empty())
        .collect();

    for segment in segments {
        if contains_hiragana_katakana(segment) {
            // Japanese text: extract script-specific chunks
            for m in JP_PART_RE.find_iter(segment) {
                let part = m.as_str();
                let chars: Vec<char> = part.chars().collect();
                if chars.iter().all(|c| is_cjk_char(*c)) {
                    // CJK: add whole + bigrams
                    tokens.push(part.to_string());
                    for i in 0..chars.len().saturating_sub(1) {
                        let bigram: String = [chars[i], chars[i + 1]].iter().collect();
                        tokens.push(bigram);
                    }
                } else {
                    tokens.push(part.to_string());
                }
            }
        } else if contains_cjk(segment) {
            // Chinese: character n-grams
            let chars: Vec<char> = segment.chars().filter(|c| is_cjk_char(*c)).collect();
            for &c in &chars {
                tokens.push(c.to_string());
            }
            for i in 0..chars.len().saturating_sub(1) {
                let bigram: String = [chars[i], chars[i + 1]].iter().collect();
                tokens.push(bigram);
            }
        } else if contains_hangul(segment) {
            // Korean
            let stem = strip_korean_trailing_particle(segment);
            let stem_is_stop = stem
                .as_ref()
                .is_some_and(|s| STOP_WORDS_KO.contains(s.as_str()));
            if !STOP_WORDS_KO.contains(segment) && !stem_is_stop {
                tokens.push(segment.to_string());
            }
            if let Some(ref s) = stem {
                if !STOP_WORDS_KO.contains(s.as_str()) && is_useful_korean_stem(s) {
                    tokens.push(s.clone());
                }
            }
        } else {
            tokens.push(segment.to_string());
        }
    }

    tokens
}

/// Extract meaningful keywords from a conversational query for FTS search.
pub fn extract_keywords(query: &str) -> Vec<String> {
    let tokens = tokenize(query);
    let mut keywords = Vec::new();
    let mut seen = std::collections::HashSet::new();

    for token in tokens {
        if is_query_stop_word_token(&token) {
            continue;
        }
        if !is_valid_keyword(&token) {
            continue;
        }
        if seen.contains(&token) {
            continue;
        }
        seen.insert(token.clone());
        keywords.push(token);
    }

    keywords
}

/// Expand a query for FTS search.
/// Returns the original query, extracted keywords, and an expanded OR query.
pub fn expand_query_for_fts(query: &str) -> ExpandedQuery {
    let original = query.trim().to_string();
    let keywords = extract_keywords(&original);

    let expanded = if keywords.is_empty() {
        original.clone()
    } else {
        format!("{} OR {}", original, keywords.join(" OR "))
    };

    ExpandedQuery {
        original,
        keywords,
        expanded,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_english_stop_words() {
        assert!(is_query_stop_word_token("the"));
        assert!(is_query_stop_word_token("is"));
        assert!(is_query_stop_word_token("yesterday"));
        assert!(!is_query_stop_word_token("algorithm"));
    }

    #[test]
    fn test_chinese_stop_words() {
        assert!(is_query_stop_word_token("的"));
        assert!(is_query_stop_word_token("我们"));
        assert!(is_query_stop_word_token("什么"));
    }

    #[test]
    fn test_korean_stop_words() {
        assert!(is_query_stop_word_token("그리고"));
        assert!(is_query_stop_word_token("어제"));
    }

    #[test]
    fn test_japanese_stop_words() {
        assert!(is_query_stop_word_token("これ"));
        assert!(is_query_stop_word_token("する"));
    }

    #[test]
    fn test_extract_english() {
        let kw = extract_keywords("that thing we discussed about the API");
        assert!(kw.contains(&"discussed".to_string()));
        assert!(kw.contains(&"api".to_string()));
        assert!(!kw.contains(&"the".to_string()));
        assert!(!kw.contains(&"thing".to_string()));
    }

    #[test]
    fn test_extract_chinese() {
        let kw = extract_keywords("之前讨论的那个方案");
        // Should extract character n-grams, filtering stop words
        assert!(kw.iter().any(|k| k.contains('讨') || k.contains("讨论")));
        assert!(kw.iter().any(|k| k.contains('方') || k.contains("方案")));
    }

    #[test]
    fn test_extract_empty() {
        assert!(extract_keywords("").is_empty());
        assert!(extract_keywords("the a an is").is_empty());
    }

    #[test]
    fn test_extract_korean() {
        let kw = extract_keywords("API를 설계하는 방법");
        // "API를" → stem "api", "설계하는" should have useful content
        assert!(kw.iter().any(|k| k.contains("api") || k == "api를"));
    }

    #[test]
    fn test_extract_dedup() {
        let kw = extract_keywords("test test test");
        assert_eq!(kw.len(), 1);
        assert_eq!(kw[0], "test");
    }

    #[test]
    fn test_expand_query() {
        let result = expand_query_for_fts("the API design");
        assert_eq!(result.original, "the API design");
        assert!(!result.keywords.is_empty());
        assert!(result.expanded.contains("OR"));
    }

    #[test]
    fn test_expand_query_all_stop_words() {
        let result = expand_query_for_fts("the a an");
        assert!(result.keywords.is_empty());
        assert_eq!(result.expanded, "the a an");
    }

    #[test]
    fn test_strip_korean_particle() {
        assert_eq!(
            strip_korean_trailing_particle("API를"),
            Some("API".to_string())
        );
        assert_eq!(strip_korean_trailing_particle("API"), None);
    }

    #[test]
    fn test_valid_keyword() {
        assert!(!is_valid_keyword(""));
        assert!(!is_valid_keyword("ab")); // short alpha
        assert!(!is_valid_keyword("123")); // pure digits
        assert!(is_valid_keyword("api"));
        assert!(is_valid_keyword("my_var"));
    }

    #[test]
    fn test_spanish_stop_words() {
        assert!(is_query_stop_word_token("porque"));
        assert!(is_query_stop_word_token("ayer"));
    }

    #[test]
    fn test_arabic_stop_words() {
        assert!(is_query_stop_word_token("لماذا"));
        assert!(is_query_stop_word_token("هذا"));
    }

    #[test]
    fn test_portuguese_stop_words() {
        assert!(is_query_stop_word_token("ontem"));
        assert!(is_query_stop_word_token("você"));
    }
}
