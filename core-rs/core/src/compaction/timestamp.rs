//! Timezone-aware timestamp formatting for compaction summaries.
//!
//! Matches the TypeScript `formatTimestamp` in `compaction.ts`:
//! Format: `YYYY-MM-DD HH:mm TZ`
//!
//! Uses an inline IANA-to-offset map instead of the full chrono-tz database
//! to save ~2 MB of binary size. DST is not tracked — standard offsets only.

use chrono::{DateTime, FixedOffset, TimeZone, Utc};

/// Map common IANA timezone names to (`offset_seconds_east`, abbreviation).
/// Standard offsets only; DST is not tracked.
fn iana_to_offset(tz: &str) -> Option<(i32, &'static str)> {
    Some(match tz {
        // East Asia
        "Asia/Seoul" => (32400, "KST"),
        "Asia/Tokyo" => (32400, "JST"),
        "Asia/Shanghai" | "Asia/Hong_Kong" | "Asia/Taipei" => (28800, "CST"),
        "Asia/Singapore" => (28800, "SGT"),
        // South Asia
        "Asia/Kolkata" | "Asia/Calcutta" => (19800, "IST"),
        // Europe
        "Europe/London" => (0, "GMT"),
        "Europe/Paris" | "Europe/Berlin" | "Europe/Rome" => (3600, "CET"),
        "Europe/Moscow" => (10800, "MSK"),
        // Americas
        "America/New_York" => (-18000, "EST"),
        "America/Chicago" => (-21600, "CST"),
        "America/Denver" => (-25200, "MST"),
        "America/Los_Angeles" => (-28800, "PST"),
        "America/Sao_Paulo" => (-10800, "BRT"),
        // Oceania
        "Pacific/Auckland" => (43200, "NZST"),
        "Australia/Sydney" => (36000, "AEST"),
        _ => return None,
    })
}

/// Format an epoch-millisecond timestamp as `YYYY-MM-DD HH:mm TZ`.
///
/// Falls back to UTC formatting if the timezone string is unknown.
pub fn format_timestamp(epoch_ms: i64, timezone: &str) -> String {
    let dt = DateTime::from_timestamp_millis(epoch_ms).unwrap_or_else(|| {
        Utc.timestamp_opt(0, 0)
            .single()
            .unwrap_or_else(|| unreachable!("Unix epoch timestamp is always valid"))
    });

    if timezone == "UTC" || timezone.is_empty() {
        return format!("{} UTC", dt.format("%Y-%m-%d %H:%M"));
    }

    match iana_to_offset(timezone) {
        Some((offset_secs, abbr)) => {
            let offset = FixedOffset::east_opt(offset_secs)
                .unwrap_or_else(|| unreachable!("hardcoded offsets are always valid"));
            let local = dt.with_timezone(&offset);
            format!("{} {abbr}", local.format("%Y-%m-%d %H:%M"))
        }
        None => {
            // Unknown timezone — fall back to UTC
            format!("{} UTC", dt.format("%Y-%m-%d %H:%M"))
        }
    }
}
