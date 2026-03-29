pub mod agent;
pub mod agents;
pub mod config_cmd;
pub mod configure;
pub mod dashboard;
pub mod doctor;
pub mod gateway_query;
pub mod health;
pub mod memory;
pub mod message;
pub mod onboard;
pub mod sessions;
pub mod setup;
pub mod status;

/// Domain-oriented module map to improve discoverability while keeping the
/// existing file layout and command enum stable.
pub mod domains {
    pub mod runtime {
        pub use super::super::doctor;
        pub use super::super::health;
        pub use super::super::status;
    }

    pub mod messaging {
        pub use super::super::message;
    }

    pub mod admin {
        pub use super::super::config_cmd;
        pub use super::super::configure;
        pub use super::super::onboard;
        pub use super::super::setup;
    }

    pub mod developer {
        pub use super::super::agent;
        pub use super::super::agents;
        pub use super::super::dashboard;
        pub use super::super::memory;
        pub use super::super::sessions;
    }
}
