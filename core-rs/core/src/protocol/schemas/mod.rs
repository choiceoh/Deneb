//! Per-domain schema validators.
//!
//! Each module validates RPC parameters matching the TypeScript `TypeBox` schemas
//! in `src/gateway/protocol/schema/`.

pub mod agent;
pub mod agents_models_skills;
pub mod channels;
pub mod config;
pub mod cron;
pub mod exec_approvals;
pub mod logs_chat;
pub mod secrets;
pub mod sessions;
pub mod wizard;
