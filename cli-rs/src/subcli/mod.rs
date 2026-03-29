pub mod acp;
pub mod approvals;
pub mod backup;
pub mod browser;
pub mod channels;
pub mod completion;
pub mod cron;
pub mod daemon;
pub mod devices;
pub mod directory;
pub mod docs;
pub mod gateway_cmd;
pub mod gateway_run;
pub mod hooks;
pub mod logs;
pub mod models;
pub mod nodes;
pub mod pairing;
pub mod plugins;
pub mod qr;
pub mod reset;
pub mod rpc_helpers;
pub mod sandbox;
pub mod secrets;
pub mod security;
pub mod skills;
pub mod system;
pub mod uninstall;
pub mod update;
pub mod webhooks;

/// Domain-oriented module map for subcli commands.
pub mod domains {
    pub mod messaging {
        pub use super::super::channels;
        pub use super::super::directory;
        pub use super::super::hooks;
        pub use super::super::system;
        pub use super::super::webhooks;
    }

    pub mod admin {
        pub use super::super::approvals;
        pub use super::super::backup;
        pub use super::super::reset;
        pub use super::super::secrets;
        pub use super::super::security;
        pub use super::super::uninstall;
    }

    pub mod runtime {
        pub use super::super::daemon;
        pub use super::super::gateway_cmd;
        pub use super::super::gateway_run;
        pub use super::super::logs;
        pub use super::super::sandbox;
    }

    pub mod platform {
        pub use super::super::acp;
        pub use super::super::browser;
        pub use super::super::cron;
        pub use super::super::devices;
        pub use super::super::nodes;
        pub use super::super::pairing;
        pub use super::super::qr;
    }

    pub mod developer {
        pub use super::super::completion;
        pub use super::super::docs;
        pub use super::super::models;
        pub use super::super::plugins;
        pub use super::super::skills;
        pub use super::super::update;
    }
}
