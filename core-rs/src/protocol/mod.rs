//! Gateway protocol frame validation.
//!
//! Validates RequestFrame, ResponseFrame, and EventFrame structures
//! matching the TypeScript definitions in `src/gateway/protocol/schema/frames.ts`.
//!
//! Generated protobuf types (via prost) are available in the `gen` submodule
//! when built with `cargo build` (see build.rs).

pub mod gen;

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
    InvalidField {
        field: &'static str,
        reason: String,
    },
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

/// Validate a JSON string as a gateway frame.
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
            let seq = raw.seq.and_then(|s| if s >= 0 { Some(s as u64) } else { None });
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
}
