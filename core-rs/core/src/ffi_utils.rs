//! FFI utility constants and helpers shared across all `extern "C"` exports.
//!
//! # Error code contract
//! Negative values are shared error codes (defined in `proto/gateway.proto`,
//! generated into `protocol::error_codes`). Positive return values from
//! buffer-writing functions (e.g. `deneb_detect_mime`, `deneb_sanitize_html`)
//! represent bytes written — NOT error codes.

// Re-export FFI error codes from the generated module so all FFI call sites
// (and macros defined below) continue to resolve them by the same names.
pub(crate) use crate::protocol::error_codes::{
    FFI_ERR_INPUT_TOO_LARGE, FFI_ERR_INVALID_UTF8, FFI_ERR_JSON_ERROR, FFI_ERR_NULL_POINTER,
    FFI_ERR_OUTPUT_TOO_SMALL, FFI_ERR_OVERFLOW, FFI_ERR_RUST_PANIC, FFI_ERR_VALIDATION,
};

/// Maximum input size for FFI string functions (16 MB).
/// Prevents `DoS` via pathologically large inputs.
pub(crate) const FFI_MAX_INPUT_LEN: usize = 16 * 1024 * 1024;

// Thread-local buffer holding the message from the most recent caught panic.
// Populated by `ffi_catch` and read by `deneb_get_last_panic_msg`.
std::thread_local! {
    static LAST_PANIC_MSG: std::cell::RefCell<String> = const { std::cell::RefCell::new(String::new()) };
}
/// Wraps an FFI body in `catch_unwind` to prevent Rust panics from aborting
/// the Go process. Returns `panic_rc` if the closure panics. The panic
/// message is captured in a thread-local buffer retrievable via
/// `deneb_get_last_panic_msg`.
///
/// # Safety
/// Callers must ensure the closure does not rely on invariants that could
/// be violated by unwinding. All FFI closures operate on local data only,
/// so `AssertUnwindSafe` is safe here.
pub(crate) fn ffi_catch(panic_rc: i32, f: impl FnOnce() -> i32) -> i32 {
    match std::panic::catch_unwind(std::panic::AssertUnwindSafe(f)) {
        Ok(rc) => rc,
        Err(payload) => {
            let msg = if let Some(s) = payload.downcast_ref::<String>() {
                s.clone()
            } else if let Some(s) = payload.downcast_ref::<&str>() {
                (*s).to_owned()
            } else {
                "<non-string panic payload>".to_owned()
            };
            LAST_PANIC_MSG.with(|m| *m.borrow_mut() = msg);
            panic_rc
        }
    }
}

/// Retrieves the most recent panic message stored by `ffi_catch` on the
/// current thread. Returns the number of bytes written, or a negative error
/// code if the buffer is too small. Returns 0 if no panic has been recorded.
///
/// # Safety
/// `out_ptr` must point to a buffer of at least `out_len` bytes.
#[no_mangle]
pub unsafe extern "C" fn deneb_get_last_panic_msg(out_ptr: *mut u8, out_len: usize) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_POINTER;
    }
    LAST_PANIC_MSG.with(|m| {
        let msg = m.borrow();
        if msg.is_empty() {
            return 0;
        }
        let bytes = msg.as_bytes();
        if bytes.len() > out_len {
            return FFI_ERR_OUTPUT_TOO_SMALL;
        }
        let out = std::slice::from_raw_parts_mut(out_ptr, out_len);
        out[..bytes.len()].copy_from_slice(bytes);
        bytes.len() as i32
    })
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
                let Ok($input_str) = std::str::from_utf8(input_slice) else {
                    return FFI_ERR_INVALID_UTF8;
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
                let Ok($input_str) = std::str::from_utf8(input_slice) else {
                    return FFI_ERR_INVALID_UTF8;
                };
                $body
            })
        }
    };
}

/// Write a serializable value as JSON into an output buffer.
/// Returns bytes written on success, or a negative FFI error code.
pub(crate) fn ffi_write_json(out: &mut [u8], value: &impl serde::Serialize) -> i32 {
    let Ok(json) = serde_json::to_string(value) else {
        return FFI_ERR_JSON_ERROR;
    };
    ffi_write_bytes(out, json.as_bytes())
}

/// Write raw bytes into an output buffer.
/// Returns bytes written on success, or `FFI_ERR_OUTPUT_TOO_SMALL`.
pub(crate) fn ffi_write_bytes(out: &mut [u8], data: &[u8]) -> i32 {
    if data.len() > out.len() {
        return FFI_ERR_OUTPUT_TOO_SMALL;
    }
    out[..data.len()].copy_from_slice(data);
    data.len() as i32
}

pub(crate) use ffi_string_to_buffer;
pub(crate) use ffi_string_to_int;
