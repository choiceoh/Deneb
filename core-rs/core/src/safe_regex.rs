//! `ReDoS` (Regular Expression Denial of Service) prevention.
//!
//! Ports the tokenizer + nested-repetition analyzer from
//! `src/security/safe-regex.ts` to Rust for CPU-bound safety checks.
//!
//! Provides the safety analysis (`has_nested_repetition`) in Rust.

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

#[derive(Debug, Clone)]
struct QuantifierRead {
    consumed: usize,
    min_repeat: usize,
    max_repeat: Option<usize>, // None = unbounded
}

#[derive(Debug, Clone)]
struct TokenState {
    contains_repetition: bool,
    has_ambiguous_alternation: bool,
    min_length: f64,
    max_length: f64,
}

#[derive(Debug)]
struct ParseFrame {
    last_token: Option<TokenState>,
    contains_repetition: bool,
    has_alternation: bool,
    branch_min_length: f64,
    branch_max_length: f64,
    alt_min_length: Option<f64>,
    alt_max_length: Option<f64>,
}

#[derive(Debug)]
enum PatternToken {
    Simple,
    GroupOpen,
    GroupClose,
    Alternation,
    Quantifier(QuantifierRead),
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn create_parse_frame() -> ParseFrame {
    ParseFrame {
        last_token: None,
        contains_repetition: false,
        has_alternation: false,
        branch_min_length: 0.0,
        branch_max_length: 0.0,
        alt_min_length: None,
        alt_max_length: None,
    }
}

fn add_length(left: f64, right: f64) -> f64 {
    if !left.is_finite() || !right.is_finite() {
        return f64::INFINITY;
    }
    left + right
}

fn multiply_length(length: f64, factor: usize) -> f64 {
    if !length.is_finite() {
        return if factor == 0 { 0.0 } else { f64::INFINITY };
    }
    length * factor as f64
}

fn multiply_length_opt(length: f64, factor: Option<usize>) -> f64 {
    match factor {
        None => f64::INFINITY,
        Some(f) => multiply_length(length, f),
    }
}

fn record_alternative(frame: &mut ParseFrame) {
    match (frame.alt_min_length, frame.alt_max_length) {
        (None, _) | (_, None) => {
            frame.alt_min_length = Some(frame.branch_min_length);
            frame.alt_max_length = Some(frame.branch_max_length);
        }
        (Some(alt_min), Some(alt_max)) => {
            frame.alt_min_length = Some(alt_min.min(frame.branch_min_length));
            frame.alt_max_length = Some(alt_max.max(frame.branch_max_length));
        }
    }
}

// ---------------------------------------------------------------------------
// Quantifier reader
// ---------------------------------------------------------------------------

fn read_quantifier(source: &[u8], index: usize) -> Option<QuantifierRead> {
    if index >= source.len() {
        return None;
    }
    let ch = source[index];

    let lazy_extra = |idx: usize| -> usize {
        if idx + 1 < source.len() && source[idx + 1] == b'?' {
            2
        } else {
            1
        }
    };

    match ch {
        b'*' => {
            let consumed = lazy_extra(index);
            Some(QuantifierRead {
                consumed,
                min_repeat: 0,
                max_repeat: None,
            })
        }
        b'+' => {
            let consumed = lazy_extra(index);
            Some(QuantifierRead {
                consumed,
                min_repeat: 1,
                max_repeat: None,
            })
        }
        b'?' => {
            let consumed = lazy_extra(index);
            Some(QuantifierRead {
                consumed,
                min_repeat: 0,
                max_repeat: Some(1),
            })
        }
        b'{' => parse_brace_quantifier(source, index),
        _ => None,
    }
}

fn parse_brace_quantifier(source: &[u8], index: usize) -> Option<QuantifierRead> {
    let mut i = index + 1;

    // Read min digits
    let digit_start = i;
    while i < source.len() && source[i].is_ascii_digit() {
        i += 1;
    }
    if i == digit_start {
        return None;
    }
    let min_repeat: usize = std::str::from_utf8(&source[digit_start..i])
        .ok()?
        .parse()
        .ok()?;

    let mut max_repeat: Option<usize> = Some(min_repeat);
    if i < source.len() && source[i] == b',' {
        i += 1;
        let max_start = i;
        while i < source.len() && source[i].is_ascii_digit() {
            i += 1;
        }
        max_repeat = if i == max_start {
            None
        } else {
            Some(
                std::str::from_utf8(&source[max_start..i])
                    .ok()?
                    .parse()
                    .ok()?,
            )
        };
    }

    if i >= source.len() || source[i] != b'}' {
        return None;
    }
    i += 1;
    if i < source.len() && source[i] == b'?' {
        i += 1;
    }

    if let Some(max) = max_repeat {
        if max < min_repeat {
            return None;
        }
    }

    Some(QuantifierRead {
        consumed: i - index,
        min_repeat,
        max_repeat,
    })
}

// ---------------------------------------------------------------------------
// Tokenizer
// ---------------------------------------------------------------------------

fn tokenize_pattern(source: &[u8]) -> Vec<PatternToken> {
    let mut tokens = Vec::new();
    let mut in_char_class = false;
    let mut i = 0;

    while i < source.len() {
        let ch = source[i];

        if ch == b'\\' && i + 1 < source.len() {
            i += 2; // skip escaped char
            tokens.push(PatternToken::Simple);
            continue;
        }

        if in_char_class {
            if ch == b']' {
                in_char_class = false;
            }
            i += 1;
            continue;
        }

        match ch {
            b'[' => {
                in_char_class = true;
                tokens.push(PatternToken::Simple);
            }
            b'(' => tokens.push(PatternToken::GroupOpen),
            b')' => tokens.push(PatternToken::GroupClose),
            b'|' => tokens.push(PatternToken::Alternation),
            _ => {
                if let Some(q) = read_quantifier(source, i) {
                    let consumed = q.consumed;
                    tokens.push(PatternToken::Quantifier(q));
                    i += consumed;
                    continue;
                }
                tokens.push(PatternToken::Simple);
            }
        }
        i += 1;
    }

    tokens
}

// ---------------------------------------------------------------------------
// Nested-repetition analysis
// ---------------------------------------------------------------------------

fn analyze_tokens_for_nested_repetition(tokens: &[PatternToken]) -> bool {
    let mut frames: Vec<ParseFrame> = vec![create_parse_frame()];

    for token in tokens {
        match token {
            PatternToken::Simple => {
                let frame = frames
                    .last_mut()
                    .unwrap_or_else(|| unreachable!("frame stack invariant: non-empty"));
                let ts = TokenState {
                    contains_repetition: false,
                    has_ambiguous_alternation: false,
                    min_length: 1.0,
                    max_length: 1.0,
                };
                frame.branch_min_length = add_length(frame.branch_min_length, ts.min_length);
                frame.branch_max_length = add_length(frame.branch_max_length, ts.max_length);
                frame.last_token = Some(ts);
            }
            PatternToken::GroupOpen => {
                frames.push(create_parse_frame());
            }
            PatternToken::GroupClose => {
                if frames.len() > 1 {
                    let mut closed = frames
                        .pop()
                        .unwrap_or_else(|| unreachable!("frame stack invariant: non-empty"));
                    if closed.has_alternation {
                        record_alternative(&mut closed);
                    }
                    let group_min = if closed.has_alternation {
                        closed.alt_min_length.unwrap_or(0.0)
                    } else {
                        closed.branch_min_length
                    };
                    let group_max = if closed.has_alternation {
                        closed.alt_max_length.unwrap_or(0.0)
                    } else {
                        closed.branch_max_length
                    };
                    let ts = TokenState {
                        contains_repetition: closed.contains_repetition,
                        has_ambiguous_alternation: closed.has_alternation
                            && closed.alt_min_length.is_some()
                            && closed.alt_max_length.is_some()
                            && closed.alt_min_length != closed.alt_max_length,
                        min_length: group_min,
                        max_length: group_max,
                    };
                    let frame = frames
                        .last_mut()
                        .unwrap_or_else(|| unreachable!("frame stack invariant: non-empty"));
                    if ts.contains_repetition {
                        frame.contains_repetition = true;
                    }
                    frame.branch_min_length = add_length(frame.branch_min_length, ts.min_length);
                    frame.branch_max_length = add_length(frame.branch_max_length, ts.max_length);
                    frame.last_token = Some(ts);
                }
            }
            PatternToken::Alternation => {
                let frame = frames
                    .last_mut()
                    .unwrap_or_else(|| unreachable!("frame stack invariant: non-empty"));
                frame.has_alternation = true;
                record_alternative(frame);
                frame.branch_min_length = 0.0;
                frame.branch_max_length = 0.0;
                frame.last_token = None;
            }
            PatternToken::Quantifier(q) => {
                let frame = frames
                    .last_mut()
                    .unwrap_or_else(|| unreachable!("frame stack invariant: non-empty"));
                let Some(previous) = &mut frame.last_token else {
                    continue;
                };
                if previous.contains_repetition {
                    return true;
                }
                if previous.has_ambiguous_alternation && q.max_repeat.is_none() {
                    return true;
                }

                let prev_min = previous.min_length;
                let prev_max = previous.max_length;
                previous.min_length = multiply_length(previous.min_length, q.min_repeat);
                previous.max_length = multiply_length_opt(previous.max_length, q.max_repeat);
                previous.contains_repetition = true;
                frame.contains_repetition = true;
                frame.branch_min_length = frame.branch_min_length - prev_min + previous.min_length;

                let branch_max_base = if frame.branch_max_length.is_finite() && prev_max.is_finite()
                {
                    frame.branch_max_length - prev_max
                } else {
                    f64::INFINITY
                };
                frame.branch_max_length = add_length(branch_max_base, previous.max_length);
            }
        }
    }

    false
}

// ---------------------------------------------------------------------------
// Public API (Rust)
// ---------------------------------------------------------------------------

/// Check whether a regex source pattern contains nested repetition
/// that could cause catastrophic backtracking (`ReDoS`).
pub fn has_nested_repetition_impl(source: &str) -> bool {
    analyze_tokens_for_nested_repetition(&tokenize_pattern(source.as_bytes()))
}

// ---------------------------------------------------------------------------
// napi exports
// ---------------------------------------------------------------------------

/// Check whether a regex source pattern contains nested repetition (`ReDoS` risk).
///
/// This is the Rust equivalent of `hasNestedRepetition` from `src/security/safe-regex.ts`.
pub fn has_nested_repetition(source: &str) -> bool {
    has_nested_repetition_impl(source)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn flags_nested_repetition_patterns() {
        assert!(has_nested_repetition_impl("(a+)+$"));
        assert!(has_nested_repetition_impl("(a|aa)+$"));
        assert!(!has_nested_repetition_impl("^(?:foo|bar)$"));
        assert!(!has_nested_repetition_impl("^(ab|cd)+$"));
    }

    #[test]
    fn safe_common_patterns() {
        assert!(!has_nested_repetition_impl("^agent:.*:telegram:"));
        assert!(!has_nested_repetition_impl("token=([A-Za-z0-9]+)"));
        assert!(!has_nested_repetition_impl("^agent:main$"));
    }

    #[test]
    fn rejects_classic_redos() {
        // (a+)+ — classic exponential backtrack
        assert!(has_nested_repetition_impl("(a+)+"));
        // (a*)*
        assert!(has_nested_repetition_impl("(a*)*"));
        // (a+){2,} — nested with unbounded outer
        assert!(has_nested_repetition_impl("(a+){2,}"));
    }

    #[test]
    fn allows_bounded_nested() {
        // (a|aa){2} — bounded outer quantifier on ambiguous alternation
        assert!(!has_nested_repetition_impl("(a|aa){2}$"));
    }

    #[test]
    fn handles_empty_and_simple() {
        assert!(!has_nested_repetition_impl(""));
        assert!(!has_nested_repetition_impl("abc"));
        assert!(!has_nested_repetition_impl("[a-z]+"));
        assert!(!has_nested_repetition_impl("\\d+"));
    }

    #[test]
    fn handles_character_classes() {
        // Character class contents should not be parsed as groups
        assert!(!has_nested_repetition_impl("[()]+"));
        assert!(!has_nested_repetition_impl("[a-z[A-Z]]+"));
    }

    #[test]
    fn handles_escaped_chars() {
        assert!(!has_nested_repetition_impl("\\(a+\\)+"));
        assert!(!has_nested_repetition_impl("a\\+b\\+"));
    }

    #[test]
    fn handles_trailing_backslash() {
        // Pattern ending with lone backslash should not panic.
        assert!(!has_nested_repetition_impl("foo\\"));
    }

    #[test]
    fn quantifier_parsing() {
        // Brace quantifiers
        assert!(!has_nested_repetition_impl("a{3}"));
        assert!(!has_nested_repetition_impl("a{1,5}"));
        assert!(!has_nested_repetition_impl("(ab){2,4}"));
    }
}
