//! WebSocket handshake / connection wire types.
//!
//! Mirrors `gateway-go/pkg/protocol/connect.go`.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;

use super::constants::PROTOCOL_VERSION;

/// Client handshake payload.
/// Mirrors Go `ConnectParams`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ConnectParams {
    pub min_protocol: i32,
    pub max_protocol: i32,
    pub client: ConnectClientInfo,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub caps: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub commands: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub permissions: Option<HashMap<String, bool>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub path_env: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub role: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scopes: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub device: Option<ConnectDevice>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub auth: Option<ConnectAuth>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub locale: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub user_agent: Option<String>,
}

/// Client identity.
/// Mirrors Go `ConnectClientInfo`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ConnectClientInfo {
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub display_name: Option<String>,
    pub version: String,
    pub platform: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub device_family: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model_identifier: Option<String>,
    pub mode: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub instance_id: Option<String>,
}

/// Device identity and proof for pairing.
/// Mirrors Go `ConnectDevice`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ConnectDevice {
    pub id: String,
    pub public_key: String,
    pub signature: String,
    pub signed_at: i64,
    pub nonce: String,
}

/// Authentication credentials.
/// Mirrors Go `ConnectAuth`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct ConnectAuth {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub bootstrap_token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub device_token: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub password: Option<String>,
}

/// Server handshake response.
/// Mirrors Go `HelloOk`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct HelloOk {
    #[serde(rename = "type")]
    pub hello_type: String,
    pub protocol: i32,
    pub server: HelloServer,
    pub features: HelloFeatures,
    pub snapshot: Snapshot,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub canvas_host_url: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub auth: Option<HelloAuth>,
    pub policy: HelloPolicy,
}

/// Server identity.
/// Mirrors Go `HelloServer`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct HelloServer {
    pub version: String,
    pub conn_id: String,
}

/// Available methods and events.
/// Mirrors Go `HelloFeatures`.
#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct HelloFeatures {
    pub methods: Vec<String>,
    pub events: Vec<String>,
}

/// Authentication result from the handshake.
/// Mirrors Go `HelloAuth`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct HelloAuth {
    pub device_token: String,
    pub role: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scopes: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub issued_at_ms: Option<u64>,
}

/// Server limits communicated to the client.
/// Mirrors Go `HelloPolicy`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct HelloPolicy {
    pub max_payload: u64,
    pub max_buffered_bytes: u64,
    pub tick_interval_ms: u64,
}

/// Initial state snapshot included in [`HelloOk`].
/// Mirrors Go `Snapshot`.
#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct Snapshot {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub health: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub presence: Option<Vec<PresenceEntry>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub sessions: Option<serde_json::Value>,
}

/// A connected client's presence.
/// Mirrors Go `PresenceEntry`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct PresenceEntry {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub host: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub ip: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub version: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub platform: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub device_family: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model_identifier: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,
    #[serde(rename = "lastInputSeconds", skip_serializing_if = "Option::is_none")]
    pub last_input_secs: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tags: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub text: Option<String>,
    pub ts: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub device_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub roles: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub scopes: Option<Vec<String>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub instance_id: Option<String>,
}

/// Validate required client fields in [`ConnectParams`].
///
/// Mirrors Go `ValidateConnectParams`.
pub fn validate_connect_params(params: &ConnectParams) -> Result<(), String> {
    if params.client.id.is_empty() {
        return Err("client.id is required".into());
    }
    if params.client.version.is_empty() {
        return Err("client.version is required".into());
    }
    if params.client.platform.is_empty() {
        return Err("client.platform is required".into());
    }
    if params.client.mode.is_empty() {
        return Err("client.mode is required".into());
    }
    Ok(())
}

/// Check whether the server's protocol version falls within the client's
/// supported range.
///
/// Mirrors Go `ValidateProtocolVersion`.
pub fn validate_protocol_version(params: &ConnectParams) -> bool {
    let v = PROTOCOL_VERSION as i32;
    params.min_protocol <= v && v <= params.max_protocol
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_client_info() -> ConnectClientInfo {
        ConnectClientInfo {
            id: "cli".into(),
            display_name: None,
            version: "1.0.0".into(),
            platform: "linux".into(),
            device_family: None,
            model_identifier: None,
            mode: "cli".into(),
            instance_id: None,
        }
    }

    fn sample_connect_params() -> ConnectParams {
        ConnectParams {
            min_protocol: 2,
            max_protocol: 4,
            client: sample_client_info(),
            caps: None,
            commands: None,
            permissions: None,
            path_env: None,
            role: None,
            scopes: None,
            device: None,
            auth: None,
            locale: None,
            user_agent: None,
        }
    }

    #[test]
    fn test_validate_connect_params_ok() {
        let params = sample_connect_params();
        assert!(validate_connect_params(&params).is_ok());
    }

    #[test]
    fn test_validate_connect_params_missing_id() {
        let mut params = sample_connect_params();
        params.client.id = String::new();
        assert!(validate_connect_params(&params)
            .unwrap_err()
            .contains("client.id"));
    }

    #[test]
    fn test_validate_connect_params_missing_version() {
        let mut params = sample_connect_params();
        params.client.version = String::new();
        assert!(validate_connect_params(&params)
            .unwrap_err()
            .contains("client.version"));
    }

    #[test]
    fn test_validate_protocol_version_in_range() {
        let params = sample_connect_params();
        assert!(validate_protocol_version(&params));
    }

    #[test]
    fn test_validate_protocol_version_out_of_range() {
        let mut params = sample_connect_params();
        params.min_protocol = 10;
        params.max_protocol = 20;
        assert!(!validate_protocol_version(&params));
    }

    #[test]
    fn test_connect_params_json_roundtrip() {
        let params = sample_connect_params();
        let json = serde_json::to_string(&params).expect("serialize");
        let parsed: ConnectParams = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(parsed.client.id, "cli");
        assert_eq!(parsed.min_protocol, 2);
    }

    #[test]
    fn test_hello_ok_json_roundtrip() {
        let hello = HelloOk {
            hello_type: "hello-ok".into(),
            protocol: 3,
            server: HelloServer {
                version: "1.0.0".into(),
                conn_id: "conn-1".into(),
            },
            features: HelloFeatures {
                methods: vec!["chat.send".into()],
                events: vec!["health".into()],
            },
            snapshot: Snapshot {
                health: None,
                presence: Some(vec![]),
                sessions: None,
            },
            canvas_host_url: None,
            auth: None,
            policy: HelloPolicy {
                max_payload: 25 * 1024 * 1024,
                max_buffered_bytes: 50 * 1024 * 1024,
                tick_interval_ms: 30_000,
            },
        };
        let json = serde_json::to_string(&hello).expect("serialize");
        let parsed: HelloOk = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(parsed.hello_type, "hello-ok");
        assert_eq!(parsed.protocol, 3);
        assert_eq!(parsed.server.conn_id, "conn-1");
    }

    #[test]
    fn test_presence_entry_json_field_names() {
        let entry = PresenceEntry {
            host: Some("myhost".into()),
            ip: None,
            version: Some("1.0".into()),
            platform: Some("linux".into()),
            device_family: None,
            model_identifier: None,
            mode: Some("cli".into()),
            last_input_secs: Some(42),
            reason: None,
            tags: Some(vec!["admin".into()]),
            text: None,
            ts: 1700000000,
            device_id: None,
            roles: None,
            scopes: None,
            instance_id: None,
        };
        let json = serde_json::to_value(&entry).expect("serialize");
        // Verify camelCase field names match Go wire format.
        assert_eq!(json["host"], "myhost");
        assert_eq!(json["lastInputSeconds"], 42);
        assert_eq!(json["ts"], 1700000000u64);
        assert_eq!(json["deviceFamily"], serde_json::Value::Null);
    }
}
