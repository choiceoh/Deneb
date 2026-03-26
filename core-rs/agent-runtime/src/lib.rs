//! Deneb Agent Runtime — Rust implementation of agent subsystem core logic.
//!
//! This crate provides pure-logic components from `src/agents/` that benefit
//! from Rust's performance and type safety:
//!
//! - **Model selection:** `ModelRef` parsing, normalization, provider ID resolution
//! - **Agent scope:** Agent registry, config resolution, ID normalization
//! - **Usage normalization:** Multi-provider token usage normalization
//! - **Defaults:** Default model/provider constants
//!
//! Consumed by `deneb-core` (which re-exports FFI/napi bindings).
//! Not a standalone FFI library — all external exposure goes through `deneb-core`.

#![deny(clippy::all)]

pub mod defaults;
pub mod model;
pub mod scope;
pub mod usage;
