use once_cell::sync::Lazy;
use regex::Regex;

static DATED_MEMORY_PATH_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"(?:^|/)memory/(\d{4})-(\d{2})-(\d{2})\.md$").unwrap());

/// Compute the decay constant lambda from a half-life in days.
pub fn to_decay_lambda(half_life_days: f64) -> f64 {
    if !half_life_days.is_finite() || half_life_days <= 0.0 {
        return 0.0;
    }
    std::f64::consts::LN_2 / half_life_days
}

/// Compute the temporal decay multiplier: exp(-lambda * age).
/// Returns 1.0 if decay is disabled (lambda <= 0 or invalid age).
pub fn calculate_temporal_decay_multiplier(age_in_days: f64, half_life_days: f64) -> f64 {
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
    if month < 1 || month > 12 || day < 1 || day > 31 {
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
            let epoch = NaiveDate::from_ymd_opt(1970, 1, 1).unwrap();
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_to_decay_lambda() {
        let lambda = to_decay_lambda(30.0);
        assert!((lambda - std::f64::consts::LN_2 / 30.0).abs() < 1e-10);
    }

    #[test]
    fn test_to_decay_lambda_zero() {
        assert_eq!(to_decay_lambda(0.0), 0.0);
    }

    #[test]
    fn test_to_decay_lambda_negative() {
        assert_eq!(to_decay_lambda(-5.0), 0.0);
    }

    #[test]
    fn test_multiplier_at_zero_age() {
        let m = calculate_temporal_decay_multiplier(0.0, 30.0);
        assert!((m - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_multiplier_at_half_life() {
        let m = calculate_temporal_decay_multiplier(30.0, 30.0);
        assert!((m - 0.5).abs() < 1e-10);
    }

    #[test]
    fn test_multiplier_disabled() {
        assert_eq!(calculate_temporal_decay_multiplier(10.0, 0.0), 1.0);
        assert_eq!(calculate_temporal_decay_multiplier(10.0, -1.0), 1.0);
    }

    #[test]
    fn test_apply_decay() {
        let score = apply_temporal_decay_to_score(0.8, 30.0, 30.0);
        assert!((score - 0.4).abs() < 1e-10);
    }

    #[test]
    fn test_parse_dated_path() {
        assert_eq!(
            parse_memory_date_from_path("memory/2026-01-07.md"),
            Some((2026, 1, 7))
        );
    }

    #[test]
    fn test_parse_dated_path_nested() {
        assert_eq!(
            parse_memory_date_from_path("some/dir/memory/2024-12-25.md"),
            Some((2024, 12, 25))
        );
    }

    #[test]
    fn test_parse_dated_path_backslash() {
        assert_eq!(
            parse_memory_date_from_path("memory\\2026-01-07.md"),
            Some((2026, 1, 7))
        );
    }

    #[test]
    fn test_parse_non_dated_path() {
        assert_eq!(parse_memory_date_from_path("memory/topics.md"), None);
        assert_eq!(parse_memory_date_from_path("MEMORY.md"), None);
    }

    #[test]
    fn test_parse_invalid_date() {
        assert_eq!(parse_memory_date_from_path("memory/2024-13-01.md"), None);
        assert_eq!(parse_memory_date_from_path("memory/2024-02-30.md"), None);
    }

    #[test]
    fn test_evergreen_root() {
        assert!(is_evergreen_memory_path("MEMORY.md"));
        assert!(is_evergreen_memory_path("memory.md"));
    }

    #[test]
    fn test_evergreen_topic() {
        assert!(is_evergreen_memory_path("memory/topics.md"));
        assert!(is_evergreen_memory_path("memory/api-design.md"));
    }

    #[test]
    fn test_not_evergreen_dated() {
        assert!(!is_evergreen_memory_path("memory/2026-01-07.md"));
    }

    #[test]
    fn test_not_evergreen_non_memory() {
        assert!(!is_evergreen_memory_path("src/main.rs"));
    }

    #[test]
    fn test_evergreen_dotslash() {
        assert!(is_evergreen_memory_path("./MEMORY.md"));
        assert!(is_evergreen_memory_path("./memory/topics.md"));
    }

    #[test]
    fn test_date_to_ms() {
        // 2024-01-01 UTC = 1704067200000 ms
        let ms = date_to_ms(2024, 1, 1);
        assert!((ms - 1_704_067_200_000.0).abs() < 1.0);
    }

    #[test]
    fn test_age_in_days() {
        let now = 1_704_067_200_000.0; // 2024-01-01
        let ts = now - 24.0 * 60.0 * 60.0 * 1000.0; // 1 day ago
        let age = age_in_days_from_ms(ts, now);
        assert!((age - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_age_future_timestamp_clamped() {
        // Timestamp in the future → age clamped to 0
        let now = 1_000_000.0;
        let future = 2_000_000.0;
        assert_eq!(age_in_days_from_ms(future, now), 0.0);
    }

    #[test]
    fn test_decay_nan_age() {
        assert_eq!(calculate_temporal_decay_multiplier(f64::NAN, 30.0), 1.0);
    }

    #[test]
    fn test_decay_nan_half_life() {
        assert_eq!(calculate_temporal_decay_multiplier(10.0, f64::NAN), 1.0);
    }

    #[test]
    fn test_decay_very_large_age() {
        // Very old document → multiplier approaches 0 but never negative
        let m = calculate_temporal_decay_multiplier(100_000.0, 30.0);
        assert!(m >= 0.0);
        assert!(m < 1e-10);
    }

    #[test]
    fn test_date_to_ms_invalid() {
        // Invalid date should return 0.0
        assert_eq!(date_to_ms(2024, 13, 1), 0.0);
        assert_eq!(date_to_ms(2024, 0, 1), 0.0);
    }

    #[test]
    fn test_parse_path_empty() {
        assert_eq!(parse_memory_date_from_path(""), None);
    }

    #[test]
    fn test_evergreen_empty() {
        assert!(!is_evergreen_memory_path(""));
    }
}
