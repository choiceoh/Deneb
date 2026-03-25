//! Database layer for Vega (SQLite + FTS5).
//!
//! Port of Python vega/db/: schema management, import, parsing, classification.

pub mod classify;
pub mod importer;
pub mod parser;
pub mod schema;
