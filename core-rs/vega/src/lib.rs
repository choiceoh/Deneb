//! Deneb Vega — project search engine (Rust port of Python vega/).
//!
//! Provides `SQLite` FTS5-based project search, NL command routing,
//! and hybrid BM25 + semantic search capabilities.

pub mod ai;
pub mod commands;
pub mod config;
pub mod db;
pub mod editor;
pub mod mail;
pub mod search;
pub mod session;
pub mod utils;
