//! Timezone-aware timestamp formatting for compaction summaries.
//!
//! Matches the TypeScript `formatTimestamp` in `compaction.ts`:
//! Format: `YYYY-MM-DD HH:mm TZ`

use chrono::{DateTime, TimeZone, Utc};
use chrono_tz::Tz;

/// Format an epoch-millisecond timestamp as `YYYY-MM-DD HH:mm TZ`.
///
/// Falls back to UTC formatting if the timezone string is invalid.
pub fn format_timestamp(epoch_ms: i64, timezone: &str) -> String {
    let dt = DateTime::from_timestamp_millis(epoch_ms)
        .unwrap_or_else(|| Utc.timestamp_opt(0, 0).unwrap());

    if timezone == "UTC" || timezone.is_empty() {
        return format!("{} UTC", dt.format("%Y-%m-%d %H:%M"));
    }

    match timezone.parse::<Tz>() {
        Ok(tz) => {
            let local = dt.with_timezone(&tz);
            let abbr = local.format("%Z").to_string();
            format!("{} {}", local.format("%Y-%m-%d %H:%M"), abbr)
        }
        Err(_) => {
            // Invalid timezone — fall back to UTC
            format!("{} UTC", dt.format("%Y-%m-%d %H:%M"))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_format_utc() {
        // 2024-03-25 00:00:00 UTC
        let ts = 1711324800000;
        let result = format_timestamp(ts, "UTC");
        assert_eq!(result, "2024-03-25 00:00 UTC");
    }

    #[test]
    fn test_format_empty_timezone_defaults_utc() {
        let ts = 1711324800000;
        let result = format_timestamp(ts, "");
        assert_eq!(result, "2024-03-25 00:00 UTC");
    }

    #[test]
    fn test_format_with_timezone() {
        // 2024-03-25 00:00:00 UTC = 2024-03-24 17:00 PDT (America/Los_Angeles, DST active)
        let ts = 1711324800000;
        let result = format_timestamp(ts, "America/Los_Angeles");
        assert!(result.contains("2024-03-24"));
        assert!(result.contains("17:00"));
    }

    #[test]
    fn test_format_invalid_timezone_falls_back() {
        let ts = 1711324800000;
        let result = format_timestamp(ts, "Invalid/Timezone");
        assert_eq!(result, "2024-03-25 00:00 UTC");
    }

    #[test]
    fn test_format_zero_epoch() {
        let result = format_timestamp(0, "UTC");
        assert_eq!(result, "1970-01-01 00:00 UTC");
    }
}
