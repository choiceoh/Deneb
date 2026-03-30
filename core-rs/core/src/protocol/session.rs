//! Session wire types.
//!
//! Mirrors `gateway-go/pkg/protocol/session.go`.

use serde::{Deserialize, Serialize};

/// Session run status.
/// Mirrors Go `SessionRunStatus`.
#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
pub enum SessionRunStatus {
    #[serde(rename = "running")]
    Running,
    #[serde(rename = "done")]
    Done,
    #[serde(rename = "failed")]
    Failed,
    #[serde(rename = "killed")]
    Killed,
    #[serde(rename = "timeout")]
    Timeout,
}

/// Session kind.
/// Mirrors Go `SessionKind`.
#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
pub enum SessionKind {
    #[serde(rename = "direct")]
    Direct,
    #[serde(rename = "group")]
    Group,
    #[serde(rename = "global")]
    Global,
    #[serde(rename = "unknown")]
    Unknown,
}

/// Parse a string into [`SessionKind`], defaulting to `Direct`.
///
/// Mirrors Go `ParseSessionKind`.
pub fn parse_session_kind(s: &str) -> SessionKind {
    match s {
        "group" => SessionKind::Group,
        "global" => SessionKind::Global,
        "unknown" => SessionKind::Unknown,
        _ => SessionKind::Direct,
    }
}

/// Session lifecycle phase.
/// Mirrors Go `SessionLifecyclePhase`.
#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
pub enum SessionLifecyclePhase {
    #[serde(rename = "start")]
    Start,
    #[serde(rename = "end")]
    End,
    #[serde(rename = "error")]
    Error,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_session_kind() {
        assert_eq!(parse_session_kind("group"), SessionKind::Group);
        assert_eq!(parse_session_kind("global"), SessionKind::Global);
        assert_eq!(parse_session_kind("unknown"), SessionKind::Unknown);
        assert_eq!(parse_session_kind("direct"), SessionKind::Direct);
        assert_eq!(parse_session_kind(""), SessionKind::Direct);
        assert_eq!(parse_session_kind("bogus"), SessionKind::Direct);
    }

    #[test]
    fn test_session_run_status_json() {
        assert_eq!(
            serde_json::to_string(&SessionRunStatus::Running).expect("serialize"),
            "\"running\""
        );
        assert_eq!(
            serde_json::to_string(&SessionRunStatus::Timeout).expect("serialize"),
            "\"timeout\""
        );
    }

    #[test]
    fn test_session_kind_roundtrip() {
        let kind = SessionKind::Group;
        let json = serde_json::to_string(&kind).expect("serialize");
        let parsed: SessionKind = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(parsed, SessionKind::Group);
    }

    #[test]
    fn test_lifecycle_phase_json() {
        assert_eq!(
            serde_json::to_string(&SessionLifecyclePhase::Start).expect("serialize"),
            "\"start\""
        );
        assert_eq!(
            serde_json::to_string(&SessionLifecyclePhase::Error).expect("serialize"),
            "\"error\""
        );
    }
}
