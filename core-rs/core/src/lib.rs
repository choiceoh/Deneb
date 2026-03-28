//! Deneb Core — Rust implementation of performance-critical modules.
//!
//! This crate provides:
//! - Protocol frame validation (replacing AJV)
//! - Security verification primitives + ReDoS detection
//! - Media MIME detection, EXIF parsing, PNG encoding
//!
//! It exposes both a Rust API and a C FFI surface for integration
//! with Go (via CGo) and Node.js (via napi-rs).

// The ffi module uses unsafe for C FFI exports (#[no_mangle] extern "C" functions)
// required by the Go gateway CGo integration.
#![allow(unsafe_code)]

#[cfg(feature = "napi_binding")]
#[macro_use]
extern crate napi_derive;

// Core modules (C FFI + Rust API)
pub mod compaction;
pub mod context_engine;
pub mod markdown;
pub mod media;
pub mod memory_search;
pub mod parsing;
pub mod protocol;
pub mod security;

// napi-rs modules (Node.js native addon)
pub mod exif;
pub mod external_content;
pub mod mime_utils;
pub mod png;
pub mod safe_regex;

// C FFI surface (used by Go via CGo)
mod ffi;
