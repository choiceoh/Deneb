use std::time::Duration;

use futures_util::{SinkExt, StreamExt};
use tokio::time::timeout;
use tokio_tungstenite::{connect_async, tungstenite::Message};

use crate::config::{self, DenebConfig};
use crate::errors::CliError;
use crate::gateway::auth::resolve_gateway_auth;
use crate::gateway::connection::resolve_connection_details;
use crate::gateway::protocol::{GatewayFrame, RequestFrame, ResponseFrame};

/// Options for a single gateway RPC call.
pub struct CallOptions {
    /// Explicit gateway URL override (--url).
    pub url: Option<String>,
    /// Explicit token (--token).
    pub token: Option<String>,
    /// Explicit password (--password).
    pub password: Option<String>,
    /// RPC method name.
    pub method: String,
    /// RPC parameters.
    pub params: Option<serde_json::Value>,
    /// Timeout in milliseconds.
    pub timeout_ms: u64,
    /// Whether to wait for the final response in a multi-response sequence.
    pub expect_final: bool,
}

impl Default for CallOptions {
    fn default() -> Self {
        Self {
            url: None,
            token: None,
            password: None,
            method: String::new(),
            params: None,
            timeout_ms: 10_000,
            expect_final: false,
        }
    }
}

/// Make a single RPC call to the gateway and return the response payload.
pub async fn call_gateway(opts: CallOptions) -> Result<serde_json::Value, CliError> {
    let config_path = config::resolve_config_path();
    let config = config::load_config_best_effort(&config_path);
    call_gateway_with_config(opts, &config).await
}

/// Make a single RPC call using a pre-loaded config.
pub async fn call_gateway_with_config(
    opts: CallOptions,
    config: &DenebConfig,
) -> Result<serde_json::Value, CliError> {
    let port = config::resolve_gateway_port(config.gateway_port());
    let conn = resolve_connection_details(opts.url.as_deref(), config, port);
    let auth = resolve_gateway_auth(opts.token.as_deref(), opts.password.as_deref(), config);

    // Build WebSocket URL with auth query params
    let ws_url = build_ws_url(&conn.url, &auth);

    // Connect
    let (ws_stream, _response) = timeout(
        Duration::from_millis(opts.timeout_ms),
        connect_async(&ws_url),
    )
    .await
    .map_err(|_| {
        CliError::GatewayConnection(format!(
            "connection timed out after {}ms (url: {}, source: {})",
            opts.timeout_ms, conn.url, conn.url_source
        ))
    })?
    .map_err(|e| {
        CliError::GatewayConnection(format!(
            "failed to connect to {} (source: {}): {e}",
            conn.url, conn.url_source
        ))
    })?;

    let (mut write, mut read) = ws_stream.split();

    // Send request frame
    let frame = RequestFrame::new(&opts.method, opts.params);
    let request_id = frame.id.clone();
    let frame_json = serde_json::to_string(&frame)?;
    write
        .send(Message::Text(frame_json))
        .await
        .map_err(|e| CliError::GatewayConnection(format!("failed to send request: {e}")))?;

    // Read responses until we get our correlated response
    let deadline = Duration::from_millis(opts.timeout_ms);
    let result = timeout(deadline, async {
        while let Some(msg) = read.next().await {
            let msg =
                msg.map_err(|e| CliError::GatewayConnection(format!("WebSocket read error: {e}")))?;

            let text = match msg {
                Message::Text(t) => t,
                Message::Close(_) => {
                    return Err(CliError::GatewayConnection(
                        "gateway closed the connection".to_string(),
                    ));
                }
                _ => continue, // Skip binary, ping, pong
            };

            // Try to parse as a gateway frame
            let frame: GatewayFrame = match serde_json::from_str(&text) {
                Ok(f) => f,
                Err(_) => continue, // Skip unparseable frames
            };

            match frame {
                GatewayFrame::Response(resp) => {
                    if resp.id == request_id {
                        // When expect_final is set, skip intermediate responses
                        // and wait for the final one (marked with "final": true).
                        if opts.expect_final && resp.is_final != Some(true) {
                            continue;
                        }
                        return handle_response(resp, &opts.method);
                    }
                    // Not our response, ignore
                }
                GatewayFrame::Event(_) => {
                    // Events are not request-correlated, skip
                }
            }
        }

        Err(CliError::GatewayConnection(
            "connection closed without response".to_string(),
        ))
    })
    .await
    .map_err(|_| {
        CliError::GatewayConnection(format!(
            "request timed out after {}ms (method: {})",
            opts.timeout_ms, opts.method
        ))
    })?;

    // Close the WebSocket gracefully
    let _ = write.close().await;

    result
}

/// Handle a response frame, returning the payload or an error.
fn handle_response(resp: ResponseFrame, method: &str) -> Result<serde_json::Value, CliError> {
    if resp.ok {
        Ok(resp.payload.unwrap_or(serde_json::Value::Null))
    } else {
        let error = resp.error.unwrap_or_default();
        Err(CliError::GatewayRequest {
            method: method.to_string(),
            code: error.code.unwrap_or_else(|| "UNKNOWN".to_string()),
            message: error
                .message
                .unwrap_or_else(|| "gateway request failed".to_string()),
        })
    }
}

/// Build the WebSocket URL with auth token as a query parameter.
fn build_ws_url(base_url: &str, auth: &crate::gateway::auth::GatewayAuth) -> String {
    let mut url = base_url.to_string();
    let mut has_query = url.contains('?');

    if let Some(token) = &auth.token {
        url.push(if has_query { '&' } else { '?' });
        has_query = true;
        url.push_str("token=");
        url.push_str(token);
    }

    if let Some(password) = &auth.password {
        url.push(if has_query { '&' } else { '?' });
        url.push_str("password=");
        url.push_str(password);
    }

    // Add client identification
    url.push(if has_query { '&' } else { '?' });
    url.push_str("clientName=cli-rs");
    url.push_str("&clientVersion=");
    url.push_str(crate::version::VERSION);

    url
}
