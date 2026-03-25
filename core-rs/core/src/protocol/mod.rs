//! Gateway protocol frame validation.
//!
//! Validates RequestFrame, ResponseFrame, and EventFrame structures
//! matching the TypeScript definitions in `src/gateway/protocol/schema/frames.ts`.
//!
//! Generated protobuf types (via prost) are available in the `gen` submodule
//! when built with `cargo build` (see build.rs).

pub mod constants;
pub mod error_codes;
pub mod gen;
pub mod schemas;
pub mod validation;

use serde::Deserialize;
use thiserror::Error;

#[derive(Error, Debug)]
pub enum FrameError {
    #[error("invalid JSON: {0}")]
    InvalidJson(#[from] serde_json::Error),

    #[error("unknown frame type: {0}")]
    UnknownType(String),

    #[error("missing required field: {0}")]
    MissingField(&'static str),

    #[error("invalid field value: {field} — {reason}")]
    InvalidField { field: &'static str, reason: String },
}

/// Discriminator for gateway frames.
#[derive(Deserialize, Debug, Clone, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum FrameType {
    Req,
    Res,
    Event,
}

/// A raw gateway frame before full validation.
#[derive(Deserialize, Debug)]
struct RawFrame {
    #[serde(rename = "type")]
    frame_type: FrameType,

    // RequestFrame fields
    id: Option<String>,
    method: Option<String>,

    // ResponseFrame fields
    ok: Option<bool>,

    // EventFrame fields
    event: Option<String>,
    seq: Option<i64>,

    // Shared
    payload: Option<serde_json::Value>,
    params: Option<serde_json::Value>,
    error: Option<serde_json::Value>,

    // EventFrame state version
    #[serde(rename = "stateVersion")]
    state_version: Option<StateVersion>,
}

/// State version for event frames.
#[derive(Deserialize, Debug, Clone)]
pub struct StateVersion {
    pub presence: u64,
    pub health: u64,
}

/// Validated gateway frame.
#[derive(Debug, Clone)]
pub enum GatewayFrame {
    Request(RequestFrame),
    Response(ResponseFrame),
    Event(EventFrame),
}

#[derive(Debug, Clone)]
pub struct RequestFrame {
    pub id: String,
    pub method: String,
    pub params: Option<serde_json::Value>,
}

#[derive(Debug, Clone)]
pub struct ResponseFrame {
    pub id: String,
    pub ok: bool,
    pub payload: Option<serde_json::Value>,
    pub error: Option<ErrorShape>,
}

#[derive(Deserialize, Debug, Clone)]
pub struct ErrorShape {
    pub code: String,
    pub message: String,
    pub details: Option<serde_json::Value>,
    pub retryable: Option<bool>,
    #[serde(rename = "retryAfterMs")]
    pub retry_after_ms: Option<u64>,
    pub cause: Option<String>,
}

#[derive(Debug, Clone)]
pub struct EventFrame {
    pub event: String,
    pub payload: Option<serde_json::Value>,
    pub seq: Option<u64>,
    pub state_version: Option<StateVersion>,
}

fn validate_non_empty(value: &Option<String>, field: &'static str) -> Result<String, FrameError> {
    match value {
        Some(s) if !s.is_empty() => Ok(s.clone()),
        Some(_) => Err(FrameError::InvalidField {
            field,
            reason: "must be non-empty".to_string(),
        }),
        None => Err(FrameError::MissingField(field)),
    }
}

/// A lightweight raw frame that skips deep parsing of payload/params/error.
/// Used by `validate_frame_type` for envelope-only validation.
#[derive(Deserialize)]
struct RawFrameEnvelope {
    #[serde(rename = "type")]
    frame_type: FrameType,
    id: Option<String>,
    method: Option<String>,
    ok: Option<bool>,
    event: Option<String>,
    seq: Option<i64>,
}

/// Fast envelope-only validation: returns the frame type without parsing
/// payload/params/error. Significantly cheaper than `validate_frame` for
/// callers that only need to know if the frame is well-formed.
pub fn validate_frame_type(json: &str) -> Result<FrameType, FrameError> {
    let raw: RawFrameEnvelope = serde_json::from_str(json)?;
    match raw.frame_type {
        FrameType::Req => {
            validate_non_empty(&raw.id, "id")?;
            validate_non_empty(&raw.method, "method")?;
            Ok(FrameType::Req)
        }
        FrameType::Res => {
            validate_non_empty(&raw.id, "id")?;
            raw.ok.ok_or(FrameError::MissingField("ok"))?;
            Ok(FrameType::Res)
        }
        FrameType::Event => {
            validate_non_empty(&raw.event, "event")?;
            if let Some(s) = raw.seq {
                if s < 0 {
                    return Err(FrameError::InvalidField {
                        field: "seq",
                        reason: format!("must be non-negative, got {}", s),
                    });
                }
            }
            Ok(FrameType::Event)
        }
    }
}

/// Validate a JSON string as a gateway frame (full parse including payload/params).
pub fn validate_frame(json: &str) -> Result<GatewayFrame, FrameError> {
    let raw: RawFrame = serde_json::from_str(json)?;

    match raw.frame_type {
        FrameType::Req => {
            let id = validate_non_empty(&raw.id, "id")?;
            let method = validate_non_empty(&raw.method, "method")?;
            Ok(GatewayFrame::Request(RequestFrame {
                id,
                method,
                params: raw.params,
            }))
        }
        FrameType::Res => {
            let id = validate_non_empty(&raw.id, "id")?;
            let ok = raw.ok.ok_or(FrameError::MissingField("ok"))?;
            let error = match raw.error {
                Some(v) => Some(serde_json::from_value::<ErrorShape>(v)?),
                None => None,
            };
            Ok(GatewayFrame::Response(ResponseFrame {
                id,
                ok,
                payload: raw.payload,
                error,
            }))
        }
        FrameType::Event => {
            let event = validate_non_empty(&raw.event, "event")?;
            let seq = match raw.seq {
                Some(s) if s < 0 => {
                    return Err(FrameError::InvalidField {
                        field: "seq",
                        reason: format!("must be non-negative, got {}", s),
                    });
                }
                Some(s) => Some(s as u64),
                None => None,
            };
            Ok(GatewayFrame::Event(EventFrame {
                event,
                payload: raw.payload,
                seq,
                state_version: raw.state_version,
            }))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_valid_request_frame() {
        let json = r#"{"type":"req","id":"abc","method":"chat.send","params":{"text":"hello"}}"#;
        let frame = validate_frame(json).unwrap();
        match frame {
            GatewayFrame::Request(req) => {
                assert_eq!(req.id, "abc");
                assert_eq!(req.method, "chat.send");
                assert!(req.params.is_some());
            }
            _ => panic!("expected request frame"),
        }
    }

    #[test]
    fn test_valid_response_frame() {
        let json = r#"{"type":"res","id":"abc","ok":true,"payload":{"data":1}}"#;
        let frame = validate_frame(json).unwrap();
        match frame {
            GatewayFrame::Response(res) => {
                assert_eq!(res.id, "abc");
                assert!(res.ok);
                assert!(res.error.is_none());
            }
            _ => panic!("expected response frame"),
        }
    }

    #[test]
    fn test_valid_event_frame() {
        let json = r#"{"type":"event","event":"health","seq":5}"#;
        let frame = validate_frame(json).unwrap();
        match frame {
            GatewayFrame::Event(ev) => {
                assert_eq!(ev.event, "health");
                assert_eq!(ev.seq, Some(5));
            }
            _ => panic!("expected event frame"),
        }
    }

    #[test]
    fn test_response_with_error() {
        let json = r#"{"type":"res","id":"x","ok":false,"error":{"code":"NOT_FOUND","message":"session not found","retryable":false}}"#;
        let frame = validate_frame(json).unwrap();
        match frame {
            GatewayFrame::Response(res) => {
                assert!(!res.ok);
                let err = res.error.unwrap();
                assert_eq!(err.code, "NOT_FOUND");
                assert_eq!(err.retryable, Some(false));
            }
            _ => panic!("expected response frame"),
        }
    }

    #[test]
    fn test_missing_method() {
        let json = r#"{"type":"req","id":"abc"}"#;
        assert!(validate_frame(json).is_err());
    }

    #[test]
    fn test_empty_id() {
        let json = r#"{"type":"req","id":"","method":"test"}"#;
        assert!(validate_frame(json).is_err());
    }

    #[test]
    fn test_invalid_json() {
        assert!(validate_frame("{not json}").is_err());
    }

    // --- Generated type smoke tests ---
    // Verify prost-generated types are usable and have expected fields.

    #[test]
    fn test_gen_gateway_request_frame() {
        let frame = gen::gateway::RequestFrame {
            id: "abc".into(),
            method: "chat.send".into(),
            params: None,
        };
        assert_eq!(frame.id, "abc");
        assert_eq!(frame.method, "chat.send");
    }

    #[test]
    fn test_gen_gateway_response_frame() {
        let frame = gen::gateway::ResponseFrame {
            id: "r1".into(),
            ok: true,
            payload: None,
            error: None,
        };
        assert!(frame.ok);
        assert!(frame.error.is_none());
    }

    #[test]
    fn test_gen_gateway_event_frame() {
        let frame = gen::gateway::EventFrame {
            event: "health".into(),
            payload: None,
            seq: Some(42),
            state_version: Some(gen::gateway::StateVersion {
                presence: 1,
                health: 2,
            }),
        };
        assert_eq!(frame.event, "health");
        assert_eq!(frame.seq, Some(42));
        assert_eq!(frame.state_version.unwrap().presence, 1);
    }

    #[test]
    fn test_gen_channel_capabilities() {
        let caps = gen::channel::ChannelCapabilities {
            chat_types: vec!["text".into(), "media".into()],
            polls: Some(true),
            reactions: Some(true),
            ..Default::default()
        };
        assert_eq!(caps.chat_types.len(), 2);
        assert_eq!(caps.polls, Some(true));
    }

    #[test]
    fn test_gen_session_enums() {
        // Verify enum variants exist and have distinct non-zero values.
        let running = gen::session::SessionRunStatus::Running as i32;
        let done = gen::session::SessionRunStatus::Done as i32;
        assert_ne!(running, 0, "Running should not be the proto default (0)");
        assert_ne!(done, 0, "Done should not be the proto default (0)");
        assert_ne!(running, done, "Running and Done must be distinct");

        let direct = gen::session::SessionKind::Direct as i32;
        let group = gen::session::SessionKind::Group as i32;
        assert_ne!(direct, 0);
        assert_ne!(group, 0);
        assert_ne!(direct, group);
    }

    #[test]
    fn test_gen_session_row() {
        let row = gen::session::GatewaySessionRow {
            key: "sess-1".into(),
            kind: gen::session::SessionKind::Direct as i32,
            status: gen::session::SessionRunStatus::Running as i32,
            model: Some("claude-opus-4-6".into()),
            ..Default::default()
        };
        assert_eq!(row.key, "sess-1");
        assert_eq!(row.model, Some("claude-opus-4-6".into()));
    }

    #[test]
    fn test_gen_presence_entry_roundtrip() {
        let entry = gen::gateway::PresenceEntry {
            host: Some("myhost".into()),
            ts: 1700000000,
            tags: vec!["admin".into()],
            roles: vec!["owner".into()],
            ..Default::default()
        };
        let json = serde_json::to_string(&entry).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed["host"], "myhost");
        assert_eq!(parsed["ts"], 1700000000u64);
        assert_eq!(parsed["tags"][0], "admin");
        assert_eq!(parsed["roles"][0], "owner");
    }

    #[test]
    fn test_gen_channel_roundtrip() {
        let meta = gen::channel::ChannelMeta {
            id: "telegram".into(),
            label: "Telegram".into(),
            selection_label: "Telegram Bot".into(),
            docs_path: "/channels/telegram".into(),
            blurb: "Telegram Bot API".into(),
            ..Default::default()
        };
        let json = serde_json::to_string(&meta).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed["id"], "telegram");
        assert_eq!(parsed["label"], "Telegram");
        // prost + serde uses snake_case field names by default.
        assert_eq!(parsed["selection_label"], "Telegram Bot");
    }

    #[test]
    fn test_negative_seq_rejected() {
        let json = r#"{"type":"event","event":"health","seq":-1}"#;
        let err = validate_frame(json).unwrap_err();
        assert!(err.to_string().contains("non-negative"));
    }

    #[test]
    fn test_zero_seq_accepted() {
        let json = r#"{"type":"event","event":"health","seq":0}"#;
        let frame = validate_frame(json).unwrap();
        match frame {
            GatewayFrame::Event(ev) => assert_eq!(ev.seq, Some(0)),
            _ => panic!("expected event frame"),
        }
    }

    #[test]
    fn test_extra_fields_ignored() {
        let json = r#"{"type":"req","id":"1","method":"test","unknown_field":42}"#;
        assert!(validate_frame(json).is_ok());
    }

    #[test]
    fn test_frame_type_case_sensitive() {
        let json = r#"{"type":"REQ","id":"1","method":"test"}"#;
        assert!(validate_frame(json).is_err());
    }
}
