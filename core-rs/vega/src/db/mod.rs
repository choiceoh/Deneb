//! Database layer for Vega (SQLite + FTS5).
//!
//! Port of Python vega/db/: schema management, import, parsing, classification.

pub mod schema;
pub mod parser;
pub mod classify;
pub mod importer;
