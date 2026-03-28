use serde::{Deserialize, Serialize};

/// Client → Server messages (sent over WebSocket)
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", content = "data")]
pub enum ClientMessage {
    /// Send a chat message to the agent
    SendMessage { text: String },
    /// Clear conversation history
    ClearChat,
    /// Save current session to file
    SaveSession,
    /// Set the API key (first-time setup)
    SetApiKey { key: String },
    /// Stop the current generation
    StopGeneration,
    /// Keepalive ping
    Ping,
}

/// Server → Client messages (sent over WebSocket)
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", content = "data")]
pub enum ServerMessage {
    /// Streamed text chunk from assistant
    Text { content: String },
    /// Tool execution started
    ToolStart { name: String, args: String },
    /// Tool execution completed
    ToolResult { name: String, result: String },
    /// Token usage update
    Usage {
        prompt: i32,
        completion: i32,
        total: i32,
    },
    /// Agent finished responding
    Done,
    /// Error occurred
    Error { message: String },
    /// Session saved successfully
    SessionSaved { path: String },
    /// Chat cleared
    ChatCleared,
    /// Server status after connection or config change
    ConfigStatus {
        needs_api_key: bool,
        model: String,
        service: String,
        deneb_status: String,
    },
    /// Pong response to Ping
    Pong,
}
