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

pub(crate) const FFI_ERR_NULL_POINTER: i32 = -1;
pub(crate) const FFI_ERR_INVALID_UTF8: i32 = -2;
pub(crate) const FFI_ERR_OUTPUT_TOO_SMALL: i32 = -3;
pub(crate) const FFI_ERR_INPUT_TOO_LARGE: i32 = -4;
pub(crate) const FFI_ERR_JSON_ERROR: i32 = -5;
pub(crate) const FFI_ERR_OVERFLOW: i32 = -6;
pub(crate) const FFI_ERR_VALIDATION: i32 = -7;
pub(crate) const FFI_ERR_RUST_PANIC: i32 = -99;

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

macro_rules! ffi_string_to_int {
    (
        $(#[$meta:meta])*
        fn $name:ident(
            $in_ptr:ident,
            $in_len:ident,
            max_len = $max_len:expr,
            too_large = $too_large:expr,
            $input_str:ident
        ) $body:block
    ) => {
        $(#[$meta])*
        #[no_mangle]
        pub unsafe extern "C" fn $name($in_ptr: *const u8, $in_len: usize) -> i32 {
            if $in_ptr.is_null() {
                return FFI_ERR_NULL_POINTER;
            }
            if $in_len > $max_len {
                return $too_large;
            }
            // SAFETY: pointer is null-checked above and length is bounded.
            let input_slice = std::slice::from_raw_parts($in_ptr, $in_len);
            ffi_catch(FFI_ERR_RUST_PANIC, move || {
                let $input_str = match std::str::from_utf8(input_slice) {
                    Ok(s) => s,
                    Err(_) => return FFI_ERR_INVALID_UTF8,
                };
                $body
            })
        }
    };
    (
        $(#[$meta:meta])*
        fn $name:ident($in_ptr:ident, $in_len:ident, max_len = $max_len:expr, $input_str:ident) $body:block
    ) => {
        ffi_string_to_int!(
            $(#[$meta])*
            fn $name(
                $in_ptr,
                $in_len,
                max_len = $max_len,
                too_large = FFI_ERR_INPUT_TOO_LARGE,
                $input_str
            ) $body
        );
    };
}

macro_rules! ffi_string_to_buffer {
    (
        $(#[$meta:meta])*
        fn $name:ident(
            $in_ptr:ident,
            $in_len:ident,
            out = ($out_ptr:ident, $out_len:ident),
            max_len = $max_len:expr,
            $input_str:ident,
            $out_slice:ident
        ) $body:block
    ) => {
        $(#[$meta])*
        #[no_mangle]
        pub unsafe extern "C" fn $name(
            $in_ptr: *const u8,
            $in_len: usize,
            $out_ptr: *mut u8,
            $out_len: usize,
        ) -> i32 {
            if $in_ptr.is_null() || $out_ptr.is_null() {
                return FFI_ERR_NULL_POINTER;
            }
            if $in_len > $max_len {
                return FFI_ERR_INPUT_TOO_LARGE;
            }
            // SAFETY: pointers are null-checked above and input length is bounded.
            let input_slice = std::slice::from_raw_parts($in_ptr, $in_len);
            let $out_slice = std::slice::from_raw_parts_mut($out_ptr, $out_len);
            ffi_catch(FFI_ERR_RUST_PANIC, move || {
                let $input_str = match std::str::from_utf8(input_slice) {
                    Ok(s) => s,
                    Err(_) => return FFI_ERR_INVALID_UTF8,
                };
                $body
            })
        }
    };
}

pub(crate) use ffi_string_to_buffer;
pub(crate) use ffi_string_to_int;
