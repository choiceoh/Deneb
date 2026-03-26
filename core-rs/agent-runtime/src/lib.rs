//! Deneb Agent Runtime — Rust implementation of agent subsystem core logic.
//!
//! This crate provides components from `src/agents/` ported to Rust:
//!
//! - **model:** Model selection, parsing, normalization, catalog, allowlist, thinking defaults
//! - **scope:** Agent registry, config resolution, session key parsing, account ID normalization
//! - **usage:** Multi-provider token usage normalization
//! - **defaults:** Default model/provider constants, per-mode resolution
//! - **subagent:** Subagent registry state machine (lifecycle, orphan detection)
//! - **embedded:** Embedded PI run state tracking (active runs, snapshots)
//! - **bootstrap:** Workspace bootstrap file cache
//!
//! Consumed by `deneb-core` (which re-exports FFI/napi bindings).
//! Not a standalone FFI library — all external exposure goes through `deneb-core`.

#![deny(clippy::all)]

pub mod bootstrap;
pub mod defaults;
pub mod embedded;
pub mod model;
pub mod scope;
pub mod subagent;
pub mod usage;
