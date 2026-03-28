//! FFI utility constants and helpers shared across all `extern "C"` exports.
//!
//! # Error code contract
//! Negative values are shared error codes. Positive return values from
//! buffer-writing functions (e.g. `deneb_detect_mime`, `deneb_sanitize_html`)
//! represent bytes written — NOT error codes.
//! These MUST stay in sync with `gateway-go/internal/ffi/errors.go`.

/// Maximum input size for FFI string functions (16 MB).
/// Prevents DoS via pathologically large inputs.
pub(crate) const FFI_MAX_INPUT_LEN: usize = 16 * 1024 * 1024;

pub(crate) const FFI_ERR_NULL_PTR: i32 = -1;
pub(crate) const FFI_ERR_INVALID_UTF8: i32 = -2;
pub(crate) const FFI_ERR_OUTPUT_TOO_SMALL: i32 = -3;
pub(crate) const FFI_ERR_INPUT_TOO_LARGE: i32 = -4;
pub(crate) const FFI_ERR_JSON: i32 = -5;
pub(crate) const FFI_ERR_OVERFLOW: i32 = -6;
pub(crate) const FFI_ERR_VALIDATION: i32 = -7;
pub(crate) const FFI_ERR_PANIC: i32 = -99;

/// Wraps an FFI body in `catch_unwind` to prevent Rust panics from aborting
/// the Go process. Returns `panic_rc` if the closure panics.
///
/// # Safety
/// Callers must ensure the closure does not rely on invariants that could
/// be violated by unwinding. All FFI closures operate on local data only,
/// so `AssertUnwindSafe` is safe here.
pub(crate) fn ffi_catch(panic_rc: i32, f: impl FnOnce() -> i32) -> i32 {
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(f)) {
        Ok(rc) => rc,
        Err(_) => panic_rc,
    }
}
