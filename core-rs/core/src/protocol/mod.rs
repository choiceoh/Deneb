//! Gateway protocol types — error codes and generated protobuf types.
//!
//! Frame validation and schema validation have been ported to pure Go
//! (gateway-go/internal/coreprotocol). Only error code constants and
//! protobuf codegen remain here for the active FFI modules.

pub mod error_codes;
pub mod gen;
