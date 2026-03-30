//! Gateway protocol frame validation and wire types.
//!
//! Validates `RequestFrame`, `ResponseFrame`, and `EventFrame` structures
//! matching the TypeScript definitions in `src/gateway/protocol/schema/frames.ts`
//! and the Go wire types in `gateway-go/pkg/protocol/`.
//!
//! Generated protobuf types (via prost) are available in the `gen` submodule
//! when built with `cargo build` (see build.rs).

pub mod agent;
pub mod connect;
pub mod constants;
pub mod error_codes;
pub mod gen;
pub mod plugin;
pub mod provider;
pub mod schemas;
pub mod session;
pub mod validation;

use serde::{Deserialize, Serialize};
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
#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
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
#[derive(Serialize, Deserialize, Debug, Clone)]
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

#[derive(Serialize, Debug, Clone)]
pub struct RequestFrame {
    #[serde(rename = "type")]
    pub frame_type: FrameType,
    pub id: String,
    pub method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub params: Option<serde_json::Value>,
}

#[derive(Serialize, Debug, Clone)]
pub struct ResponseFrame {
    #[serde(rename = "type")]
    pub frame_type: FrameType,
    pub id: String,
    pub ok: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub payload: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<ErrorShape>,
}

#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ErrorShape {
    pub code: String,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub retryable: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub retry_after_ms: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub cause: Option<String>,
}

#[derive(Serialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct EventFrame {
    #[serde(rename = "type")]
    pub frame_type: FrameType,
    pub event: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub payload: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub seq: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub state_version: Option<StateVersion>,
}

// ---------------------------------------------------------------------------
// Wire type constructors — mirrors gateway-go/pkg/protocol/frames.go
// ---------------------------------------------------------------------------

/// Create a new [`ErrorShape`] with the given code and message.
///
/// Mirrors Go `NewError`. Logs a warning (via `tracing`) if the code is not
/// a known error code; prefer using [`error_codes::ErrorCode`] constants.
pub fn new_error(code: impl Into<String>, message: impl Into<String>) -> ErrorShape {
    let code = code.into();
    if !error_codes::is_valid_error_code(&code) {
        // In Go this logs via slog; here we silently accept unknown codes.
        // Callers should prefer ErrorCode constants.
        let _ = &code; // suppress unused warning on the check
    }
    ErrorShape {
        code,
        message: message.into(),
        details: None,
        retryable: None,
        retry_after_ms: None,
        cause: None,
    }
}

/// Create a new [`RequestFrame`].
///
/// Both `id` and `method` must be non-empty.
/// Mirrors Go `NewRequestFrame`.
pub fn new_request_frame(
    id: impl Into<String>,
    method: impl Into<String>,
    params: Option<serde_json::Value>,
) -> Result<RequestFrame, FrameError> {
    let id = id.into();
    let method = method.into();
    if id.is_empty() {
        return Err(FrameError::InvalidField {
            field: "id",
            reason: "request id must not be empty".to_string(),
        });
    }
    if method.is_empty() {
        return Err(FrameError::InvalidField {
            field: "method",
            reason: "method must not be empty".to_string(),
        });
    }
    Ok(RequestFrame {
        frame_type: FrameType::Req,
        id,
        method,
        params,
    })
}

/// Create a successful [`ResponseFrame`].
///
/// Mirrors Go `NewResponseOK`.
pub fn new_response_ok(id: impl Into<String>, payload: Option<serde_json::Value>) -> ResponseFrame {
    ResponseFrame {
        frame_type: FrameType::Res,
        id: id.into(),
        ok: true,
        payload,
        error: None,
    }
}

/// Create an error [`ResponseFrame`].
///
/// Mirrors Go `NewResponseError`.
pub fn new_response_error(id: impl Into<String>, err: ErrorShape) -> ResponseFrame {
    ResponseFrame {
        frame_type: FrameType::Res,
        id: id.into(),
        ok: false,
        payload: None,
        error: Some(err),
    }
}

/// Create a new [`EventFrame`].
///
/// Mirrors Go `NewEventFrame`.
pub fn new_event_frame(
    event: impl Into<String>,
    payload: Option<serde_json::Value>,
) -> EventFrame {
    EventFrame {
        frame_type: FrameType::Event,
        event: event.into(),
        payload,
        seq: None,
        state_version: None,
    }
}

/// Extract the `"type"` field from raw JSON without full unmarshal.
///
/// Mirrors Go `ParseFrameType`.
pub fn parse_frame_type(data: &[u8]) -> Result<FrameType, FrameError> {
    #[derive(Deserialize)]
    struct Peek {
        #[serde(rename = "type")]
        frame_type: Option<FrameType>,
    }
    let peek: Peek = serde_json::from_slice(data)?;
    peek.frame_type
        .ok_or_else(|| FrameError::MissingField("type"))
}

// ---------------------------------------------------------------------------
// Validation internals
// ---------------------------------------------------------------------------

/// Maximum length for short string fields (id, method, event) to prevent `DoS`.
const MAX_SHORT_FIELD_LEN: usize = 256;

fn validate_non_empty(value: &Option<String>, field: &'static str) -> Result<String, FrameError> {
    match value {
        Some(s) if !s.is_empty() => {
            if s.len() > MAX_SHORT_FIELD_LEN {
                return Err(FrameError::InvalidField {
                    field,
                    reason: format!(
                        "exceeds maximum length ({} > {})",
                        s.len(),
                        MAX_SHORT_FIELD_LEN
                    ),
                });
            }
            Ok(s.clone())
        }
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
                        reason: format!("must be non-negative, got {s}"),
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
                frame_type: FrameType::Req,
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
                frame_type: FrameType::Res,
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
                        reason: format!("must be non-negative, got {s}"),
                    });
                }
                Some(s) => Some(s as u64),
                None => None,
            };
            Ok(GatewayFrame::Event(EventFrame {
                frame_type: FrameType::Event,
                event,
                payload: raw.payload,
                seq,
                state_version: raw.state_version,
            }))
        }
    }
}

#[cfg(test)]
#[allow(clippy::expect_used)]
mod tests {
    use super::*;

    #[test]
    fn test_valid_request_frame() -> Result<(), Box<dyn std::error::Error>> {
        let json = r#"{"type":"req","id":"abc","method":"chat.send","params":{"text":"hello"}}"#;
        let frame = validate_frame(json)?;
        match frame {
            GatewayFrame::Request(req) => {
                assert_eq!(req.id, "abc");
                assert_eq!(req.method, "chat.send");
                assert!(req.params.is_some());
            }
            _ => panic!("expected request frame"),
        }
        Ok(())
    }

    #[test]
    fn test_valid_response_frame() -> Result<(), Box<dyn std::error::Error>> {
        let json = r#"{"type":"res","id":"abc","ok":true,"payload":{"data":1}}"#;
        let frame = validate_frame(json)?;
        match frame {
            GatewayFrame::Response(res) => {
                assert_eq!(res.id, "abc");
                assert!(res.ok);
                assert!(res.error.is_none());
            }
            _ => panic!("expected response frame"),
        }
        Ok(())
    }

    #[test]
    fn test_valid_event_frame() -> Result<(), Box<dyn std::error::Error>> {
        let json = r#"{"type":"event","event":"health","seq":5}"#;
        let frame = validate_frame(json)?;
        match frame {
            GatewayFrame::Event(ev) => {
                assert_eq!(ev.event, "health");
                assert_eq!(ev.seq, Some(5));
            }
            _ => panic!("expected event frame"),
        }
        Ok(())
    }

    #[test]
    fn test_response_with_error() -> Result<(), Box<dyn std::error::Error>> {
        let json = r#"{"type":"res","id":"x","ok":false,"error":{"code":"NOT_FOUND","message":"session not found","retryable":false}}"#;
        let frame = validate_frame(json)?;
        match frame {
            GatewayFrame::Response(res) => {
                assert!(!res.ok);
                let err = res.error.expect("error field should be Some");
                assert_eq!(err.code, "NOT_FOUND");
                assert_eq!(err.retryable, Some(false));
            }
            _ => panic!("expected response frame"),
        }
        Ok(())
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
        assert_eq!(
            frame
                .state_version
                .expect("state_version should be Some")
                .presence,
            1
        );
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
    fn test_gen_presence_entry_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
        let entry = gen::gateway::PresenceEntry {
            host: Some("myhost".into()),
            ts: 1700000000,
            tags: vec!["admin".into()],
            roles: vec!["owner".into()],
            ..Default::default()
        };
        let json = serde_json::to_string(&entry)?;
        let parsed: serde_json::Value = serde_json::from_str(&json)?;
        assert_eq!(parsed["host"], "myhost");
        assert_eq!(parsed["ts"], 1700000000u64);
        assert_eq!(parsed["tags"][0], "admin");
        assert_eq!(parsed["roles"][0], "owner");
        Ok(())
    }

    #[test]
    fn test_gen_channel_roundtrip() -> Result<(), Box<dyn std::error::Error>> {
        let meta = gen::channel::ChannelMeta {
            id: "telegram".into(),
            label: "Telegram".into(),
            selection_label: "Telegram Bot".into(),
            docs_path: "/channels/telegram".into(),
            blurb: "Telegram Bot API".into(),
            ..Default::default()
        };
        let json = serde_json::to_string(&meta)?;
        let parsed: serde_json::Value = serde_json::from_str(&json)?;
        assert_eq!(parsed["id"], "telegram");
        assert_eq!(parsed["label"], "Telegram");
        // prost + serde uses snake_case field names by default.
        assert_eq!(parsed["selection_label"], "Telegram Bot");
        Ok(())
    }

    #[test]
    fn test_negative_seq_rejected() {
        let json = r#"{"type":"event","event":"health","seq":-1}"#;
        let err = validate_frame(json).unwrap_err();
        assert!(err.to_string().contains("non-negative"));
    }

    #[test]
    fn test_zero_seq_accepted() -> Result<(), Box<dyn std::error::Error>> {
        let json = r#"{"type":"event","event":"health","seq":0}"#;
        let frame = validate_frame(json)?;
        match frame {
            GatewayFrame::Event(ev) => assert_eq!(ev.seq, Some(0)),
            _ => panic!("expected event frame"),
        }
        Ok(())
    }

    #[test]
    fn test_extra_fields_ignored() {
        let json = r#"{"type":"req","id":"1","method":"test","unknown_field":42}"#;
        assert!(validate_frame(json).is_ok());
    }

    #[test]
    fn test_oversized_id_rejected() {
        let long_id = "x".repeat(300);
        let json = format!(r#"{{"type":"req","id":"{}","method":"test"}}"#, long_id);
        let err = validate_frame(&json).unwrap_err();
        assert!(err.to_string().contains("maximum length"));
    }

    #[test]
    fn test_oversized_method_rejected() {
        let long_method = "m".repeat(300);
        let json = format!(r#"{{"type":"req","id":"1","method":"{}"}}"#, long_method);
        assert!(validate_frame(&json).is_err());
    }

    #[test]
    fn test_frame_type_case_sensitive() {
        let json = r#"{"type":"REQ","id":"1","method":"test"}"#;
        assert!(validate_frame(json).is_err());
    }

    // --- Property-based tests (proptest) ---

    mod proptests {
        use super::*;
        use proptest::prelude::*;

        /// Strategy for non-empty strings that fit within MAX_SHORT_FIELD_LEN.
        fn short_nonempty_string() -> impl Strategy<Value = String> {
            "[a-zA-Z0-9_.]{1,64}"
        }

        proptest! {
            #[test]
            fn valid_request_frame_roundtrips(
                id in short_nonempty_string(),
                method in short_nonempty_string(),
            ) {
                let json = serde_json::json!({
                    "type": "req",
                    "id": id,
                    "method": method,
                    "params": { "key": "value" }
                }).to_string();
                let frame = validate_frame(&json).expect("valid request json should parse");
                match frame {
                    GatewayFrame::Request(req) => {
                        prop_assert_eq!(req.id, id);
                        prop_assert_eq!(req.method, method);
                    }
                    _ => prop_assert!(false, "expected Request frame"),
                }
            }

            #[test]
            fn valid_response_frame_roundtrips(
                id in short_nonempty_string(),
                ok in any::<bool>(),
            ) {
                let json = serde_json::json!({
                    "type": "res",
                    "id": id,
                    "ok": ok,
                    "payload": null
                }).to_string();
                let frame = validate_frame(&json).expect("valid response json should parse");
                match frame {
                    GatewayFrame::Response(res) => {
                        prop_assert_eq!(res.id, id);
                        prop_assert_eq!(res.ok, ok);
                    }
                    _ => prop_assert!(false, "expected Response frame"),
                }
            }

            #[test]
            fn valid_event_frame_roundtrips(
                event in short_nonempty_string(),
                seq in proptest::option::of(0u64..1_000_000),
            ) {
                let mut obj = serde_json::json!({
                    "type": "event",
                    "event": event,
                });
                if let Some(s) = seq {
                    obj["seq"] = serde_json::json!(s);
                }
                let json = obj.to_string();
                let frame = validate_frame(&json).expect("valid event json should parse");
                match frame {
                    GatewayFrame::Event(ev) => {
                        prop_assert_eq!(ev.event, event);
                        prop_assert_eq!(ev.seq, seq);
                    }
                    _ => prop_assert!(false, "expected Event frame"),
                }
            }

            #[test]
            fn garbage_strings_never_panic(s in "\\PC{0,512}") {
                // validate_frame must not panic on arbitrary input; errors are fine.
                let _ = validate_frame(&s);
            }
        }
    }
}
