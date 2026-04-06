//! Gateway protocol frame validation.
//!
//! Validates `RequestFrame`, `ResponseFrame`, and `EventFrame` structures
//! matching the TypeScript definitions in `src/gateway/protocol/schema/frames.ts`.
//!
//! Generated protobuf types (via prost) are available in the `gen` submodule
//! when built with `cargo build` (see build.rs).

pub mod constants;
pub mod error_codes;
pub mod gen;
#[macro_use]
pub mod validation;
pub mod schemas;

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
                        reason: format!("must be non-negative, got {s}"),
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
