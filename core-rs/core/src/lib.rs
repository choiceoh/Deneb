//! Deneb Core — Rust implementation of performance-critical modules.
//!
//! This crate provides:
//! - Memory search (SIMD cosine, BM25, FTS)
//! - Context engine and compaction state machines
//!
//! It exposes a C FFI surface for integration with Go (via `CGo`).
//!
//! Modules ported to pure Go (no longer in this crate):
//! protocol, markdown, parsing, media, security.

// This crate uses unsafe for C FFI exports (#[no_mangle] extern "C" functions)
// required by the Go gateway CGo integration.
#![allow(unsafe_code)]

// FFI utilities: error codes, FFI_MAX_INPUT_LEN, ffi_catch
mod ffi_utils;

// Core modules (C FFI + Rust API)
pub mod compaction;
pub mod context_engine;
pub mod memory_search;
pub mod protocol;

// C FFI exports organised by domain (used by Go via CGo).
// Each submodule in ffi/ owns the `deneb_*` functions for its domain.
mod ffi;

// Re-export all FFI symbols into the crate root so that existing callers
// and tests that do `use super::*` continue to resolve them without changes.
pub use ffi::compaction::*;
pub use ffi::context_engine::*;
pub use ffi::memory_search::*;
pub use ffi::ml::*;
pub use ffi::vega::*;
