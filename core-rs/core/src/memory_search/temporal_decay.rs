//! Temporal decay scoring — applies recency bias to memory search results.
//!
//! Uses exponential decay: `score × exp(−λ × age_days)` where λ = ln(2) / `half_life`.
//! Memories older than the half-life have their score halved; evergreen files
//! (e.g., `MEMORY.md`) are exempt from decay.

use regex::Regex;
use std::sync::LazyLock;

#[allow(clippy::expect_used)]
static DATED_MEMORY_PATH_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?:^|/)memory/(\d{4})-(\d{2})-(\d{2})\.md$").expect("valid regex")
});

/// Compute the decay constant lambda from a half-life in days.
fn to_decay_lambda(half_life_days: f64) -> f64 {
    if !half_life_days.is_finite() || half_life_days <= 0.0 {
        return 0.0;
    }
    std::f64::consts::LN_2 / half_life_days
}

/// Compute the temporal decay multiplier: exp(-lambda * age).
/// Returns 1.0 if decay is disabled (lambda <= 0 or invalid age).
fn calculate_temporal_decay_multiplier(age_in_days: f64, half_life_days: f64) -> f64 {
    let lambda = to_decay_lambda(half_life_days);
    let clamped_age = age_in_days.max(0.0);
    if lambda <= 0.0 || !clamped_age.is_finite() {
        return 1.0;
    }
    (-lambda * clamped_age).exp()
}

/// Apply temporal decay to a score.
pub fn apply_temporal_decay_to_score(score: f64, age_in_days: f64, half_life_days: f64) -> f64 {
    score * calculate_temporal_decay_multiplier(age_in_days, half_life_days)
}

/// Parse a date (year, month, day) from a memory file path.
/// Matches paths like `memory/2026-01-07.md`.
/// Returns `None` if the path doesn't contain a dated memory pattern or the date is invalid.
pub fn parse_memory_date_from_path(file_path: &str) -> Option<(i32, u32, u32)> {
    let normalized = file_path.replace('\\', "/");
    let normalized = normalized.strip_prefix("./").unwrap_or(&normalized);

    let caps = DATED_MEMORY_PATH_RE.captures(normalized)?;
    let year: i32 = caps.get(1)?.as_str().parse().ok()?;
    let month: u32 = caps.get(2)?.as_str().parse().ok()?;
    let day: u32 = caps.get(3)?.as_str().parse().ok()?;

    // Validate the date
    if !(1..=12).contains(&month) || !(1..=31).contains(&day) {
        return None;
    }

    // More precise validation: check days in month
    let days_in_month = match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 => {
            if (year % 4 == 0 && year % 100 != 0) || year % 400 == 0 {
                29
            } else {
                28
            }
        }
        _ => return None,
    };
    if day > days_in_month {
        return None;
    }

    Some((year, month, day))
}

/// Check if a memory file path is "evergreen" (not date-specific).
/// Evergreen files like `MEMORY.md`, `memory.md`, or `memory/topics.md` should not decay.
pub fn is_evergreen_memory_path(file_path: &str) -> bool {
    let normalized = file_path.replace('\\', "/");
    let normalized = normalized.strip_prefix("./").unwrap_or(&normalized);

    if normalized == "MEMORY.md" || normalized == "memory.md" {
        return true;
    }
    if !normalized.starts_with("memory/") {
        return false;
    }
    !DATED_MEMORY_PATH_RE.is_match(normalized)
}

/// Convert a (year, month, day) tuple to milliseconds since Unix epoch (UTC midnight).
pub fn date_to_ms(year: i32, month: u32, day: u32) -> f64 {
    // Use a simple calculation: days from epoch to the given date
    // This mirrors JavaScript's Date.UTC(year, month-1, day)
    use chrono::NaiveDate;
    let date = NaiveDate::from_ymd_opt(year, month, day);
    match date {
        Some(d) => {
            let epoch = NaiveDate::from_ymd_opt(1970, 1, 1)
                .unwrap_or_else(|| unreachable!("1970-01-01 is always valid"));
            let days = (d - epoch).num_days();
            days as f64 * 24.0 * 60.0 * 60.0 * 1000.0
        }
        None => 0.0,
    }
}

/// Compute age in days from a timestamp (ms) and current time (ms).
pub fn age_in_days_from_ms(timestamp_ms: f64, now_ms: f64) -> f64 {
    let age_ms = (now_ms - timestamp_ms).max(0.0);
    age_ms / (24.0 * 60.0 * 60.0 * 1000.0)
}
