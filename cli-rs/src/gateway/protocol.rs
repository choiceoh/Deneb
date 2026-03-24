use serde::{Deserialize, Serialize};

/// Outgoing RPC request frame (JSON wire format).
#[derive(Debug, Serialize)]
pub struct RequestFrame {
    #[serde(rename = "type")]
    pub frame_type: &'static str,
    pub id: String,
    pub method: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub params: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scopes: Option<Vec<String>>,
}

impl RequestFrame {
    pub fn new(method: impl Into<String>, params: Option<serde_json::Value>) -> Self {
        Self {
            frame_type: "req",
            id: uuid::Uuid::new_v4().to_string(),
            method: method.into(),
            params,
            scopes: None,
        }
    }

    pub fn with_scopes(mut self, scopes: Vec<String>) -> Self {
        self.scopes = Some(scopes);
        self
    }
}

/// Incoming response frame from the gateway.
#[derive(Debug, Deserialize)]
pub struct ResponseFrame {
    pub id: String,
    pub ok: bool,
    #[serde(default)]
    pub payload: Option<serde_json::Value>,
    #[serde(default)]
    pub error: Option<ErrorShape>,
    /// Whether this is the final response in a multi-response sequence.
    #[serde(default, rename = "final")]
    pub is_final: Option<bool>,
}

/// Incoming event frame from the gateway (not request-correlated).
#[derive(Debug, Deserialize)]
pub struct EventFrame {
    pub event: String,
    #[serde(default)]
    pub payload: Option<serde_json::Value>,
    #[serde(default)]
    pub seq: Option<u64>,
}

/// Error shape in a gateway response.
#[derive(Debug, Clone, Deserialize)]
pub struct ErrorShape {
    #[serde(default)]
    pub code: Option<String>,
    #[serde(default)]
    pub message: Option<String>,
    #[serde(default)]
    pub details: Option<serde_json::Value>,
}

/// A gateway frame that can be either a response or an event.
#[derive(Debug, Deserialize)]
#[serde(tag = "type")]
pub enum GatewayFrame {
    #[serde(rename = "res")]
    Response(ResponseFrame),
    #[serde(rename = "evt")]
    Event(EventFrame),
}

/// Connect params sent in the WebSocket URL query or initial hello.
#[derive(Debug, Default, Serialize)]
pub struct ConnectParams {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub password: Option<String>,
    #[serde(rename = "clientName", skip_serializing_if = "Option::is_none")]
    pub client_name: Option<String>,
    #[serde(rename = "clientVersion", skip_serializing_if = "Option::is_none")]
    pub client_version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub platform: Option<String>,
}

/// Protocol version supported by this CLI.
pub const PROTOCOL_VERSION: u32 = 1;
