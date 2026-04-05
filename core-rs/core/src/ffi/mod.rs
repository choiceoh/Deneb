//! C FFI exports for Go/CGo integration (`deneb_*` functions).
//!
//! Each submodule groups related exports by domain. `lib.rs` re-exports
//! all symbols into the crate root so existing callers and tests are unchanged.

pub mod compaction;
pub mod context_engine;
pub mod markdown;
pub mod media;
pub mod memory_search;
pub mod ml;
pub mod parsing;
pub mod protocol;
pub mod security;
pub mod vega;
