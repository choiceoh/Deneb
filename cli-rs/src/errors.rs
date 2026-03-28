use thiserror::Error;

#[derive(Error, Debug)]
pub enum CliError {
    #[error("gateway connection failed: {0}")]
    GatewayConnection(String),

    #[error("gateway request failed: {method} — {message}")]
    GatewayRequest {
        method: String,
        code: String,
        message: String,
    },

    #[error("config error: {0}")]
    Config(String),

    #[error("config file not found: {0}")]
    #[allow(dead_code)]
    ConfigNotFound(String),

    #[error("{0}")]
    User(String),

    #[error(transparent)]
    Io(#[from] std::io::Error),

    #[error(transparent)]
    Json(#[from] serde_json::Error),

    #[error(transparent)]
    Other(#[from] anyhow::Error),
}

impl CliError {
    /// Format the error for user-facing output (no backtrace, no "Error:" prefix duplication).
    pub fn user_message(&self) -> String {
        match self {
            CliError::GatewayRequest {
                method,
                code,
                message,
            } => {
                format!("Gateway {method} failed [{code}]: {message}")
            }
            other => format!("{other}"),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::CliError;

    #[test]
    fn user_message_formats_gateway_request_with_context() {
        let error = CliError::GatewayRequest {
            method: String::from("sessions.list"),
            code: String::from("UNAUTHORIZED"),
            message: String::from("token missing"),
        };

        assert_eq!(
            error.user_message(),
            "Gateway sessions.list failed [UNAUTHORIZED]: token missing"
        );
    }

    #[test]
    fn user_message_uses_display_for_non_gateway_errors() {
        let error = CliError::GatewayConnection(String::from("connection reset"));

        assert_eq!(
            error.user_message(),
            "gateway connection failed: connection reset"
        );
    }
}
