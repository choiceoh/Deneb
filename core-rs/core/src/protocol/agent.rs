//! Agent execution wire types.
//!
//! Mirrors `gateway-go/pkg/protocol/agent.go`.

use serde::{Deserialize, Serialize};

/// Agent execution lifecycle status.
/// Mirrors Go `AgentStatus`.
#[derive(Serialize, Deserialize, Debug, Clone, PartialEq, Eq)]
pub enum AgentStatus {
    #[serde(rename = "")]
    Unspecified,
    #[serde(rename = "spawning")]
    Spawning,
    #[serde(rename = "running")]
    Running,
    #[serde(rename = "completed")]
    Completed,
    #[serde(rename = "failed")]
    Failed,
    #[serde(rename = "killed")]
    Killed,
}

impl AgentStatus {
    /// Returns `true` if the status represents a final (terminal) state.
    /// Mirrors Go `AgentStatus.IsTerminal()`.
    pub fn is_terminal(&self) -> bool {
        matches!(
            self,
            AgentStatus::Completed | AgentStatus::Failed | AgentStatus::Killed
        )
    }
}

/// Request to spawn a new agent execution.
/// Mirrors Go `AgentSpawnRequest`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct AgentSpawnRequest {
    pub session_key: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub model: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub provider: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub prompt: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub thinking_level: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub parent_session_key: Option<String>,
}

/// Agent execution state change report.
/// Mirrors Go `AgentStatusUpdate`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct AgentStatusUpdate {
    pub session_key: String,
    pub status: AgentStatus,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub timestamp_ms: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
}

/// Final result of an agent execution.
/// Mirrors Go `AgentExecutionResult`.
#[derive(Serialize, Deserialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct AgentExecutionResult {
    pub session_key: String,
    pub final_status: AgentStatus,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub runtime_ms: Option<i64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub input_tokens: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output_tokens: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub estimated_cost_usd: Option<f64>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_agent_status_terminal() {
        assert!(!AgentStatus::Unspecified.is_terminal());
        assert!(!AgentStatus::Spawning.is_terminal());
        assert!(!AgentStatus::Running.is_terminal());
        assert!(AgentStatus::Completed.is_terminal());
        assert!(AgentStatus::Failed.is_terminal());
        assert!(AgentStatus::Killed.is_terminal());
    }

    #[test]
    fn test_agent_status_json_values() {
        assert_eq!(
            serde_json::to_string(&AgentStatus::Running).expect("serialize"),
            "\"running\""
        );
        assert_eq!(
            serde_json::to_string(&AgentStatus::Unspecified).expect("serialize"),
            "\"\""
        );
    }

    #[test]
    fn test_agent_spawn_request_roundtrip() {
        let req = AgentSpawnRequest {
            session_key: "sess-1".into(),
            model: Some("claude-opus-4-6".into()),
            provider: None,
            prompt: Some("hello".into()),
            thinking_level: None,
            parent_session_key: None,
        };
        let json = serde_json::to_string(&req).expect("serialize");
        let parsed: AgentSpawnRequest = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(parsed.session_key, "sess-1");
        assert_eq!(parsed.model.as_deref(), Some("claude-opus-4-6"));
    }

    #[test]
    fn test_agent_execution_result_roundtrip() {
        let result = AgentExecutionResult {
            session_key: "sess-1".into(),
            final_status: AgentStatus::Completed,
            output: Some("done".into()),
            runtime_ms: Some(1234),
            input_tokens: Some(100),
            output_tokens: Some(50),
            estimated_cost_usd: Some(0.01),
        };
        let json = serde_json::to_string(&result).expect("serialize");
        let parsed: AgentExecutionResult = serde_json::from_str(&json).expect("deserialize");
        assert_eq!(parsed.final_status, AgentStatus::Completed);
        assert_eq!(parsed.runtime_ms, Some(1234));
    }
}
