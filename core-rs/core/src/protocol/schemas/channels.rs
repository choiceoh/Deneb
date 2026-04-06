//! Channel schema validators — mirrors `src/gateway/protocol/schema/channels.ts`.

use crate::protocol::validation::*;

define_schema! {
    pub fn validate_talk_mode_params {
        [req "enabled" => boolean],
        [opt "phase" => string],
    }
}

define_schema! {
    pub fn validate_talk_config_params {
        [opt "includeSecrets" => boolean],
    }
}

define_schema! {
    pub fn validate_channels_status_params {
        [opt "probe" => boolean],
        [opt "timeoutMs" => integer(Some(0), None)],
    }
}

define_schema! {
    pub fn validate_channels_logout_params {
        [req "channel" => non_empty_string],
        [opt "accountId" => string],
    }
}

define_schema! {
    pub fn validate_web_login_start_params {
        [opt "force" => boolean],
        [opt "timeoutMs" => integer(Some(0), None)],
        [opt "verbose" => boolean],
        [opt "accountId" => string],
    }
}

define_schema! {
    pub fn validate_web_login_wait_params {
        [opt "timeoutMs" => integer(Some(0), None)],
        [opt "accountId" => string],
    }
}
