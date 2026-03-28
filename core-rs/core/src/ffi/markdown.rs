//! Markdown IR parsing and fence detection FFI exports.

#![allow(unsafe_code)]

use super::helpers::{ffi_read_str, ffi_write_json};
use super::*;
use crate::markdown;

/// C FFI: Parse markdown text into a MarkdownIR structure.
/// Takes markdown text and an optional JSON options string.
/// Writes JSON `{"text":"...","styles":[...],"links":[...],"has_code_blocks":bool}` to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `md_ptr` must be valid UTF-8. `out_ptr` must be writable for `out_len` bytes.
/// `opts_ptr` may be null for default options.
#[no_mangle]
pub unsafe extern "C" fn deneb_markdown_to_ir(
    md_ptr: *const u8,
    md_len: usize,
    opts_ptr: *const u8,
    opts_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if md_ptr.is_null() || out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    if md_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    // SAFETY: md_ptr and out_ptr are null-checked above, md_len bounded by
    // FFI_MAX_INPUT_LEN. The Go caller guarantees both buffers are valid.
    let md_slice = std::slice::from_raw_parts(md_ptr, md_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let md_str = match std::str::from_utf8(md_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let options = if !opts_ptr.is_null() && opts_len > 0 {
            // SAFETY: opts_ptr is non-null (checked in this branch), opts_len > 0.
            let opts_bytes = std::slice::from_raw_parts(opts_ptr, opts_len);
            match std::str::from_utf8(opts_bytes) {
                Ok(s) => match serde_json::from_str::<markdown::parser::ParseOptions>(s) {
                    Ok(o) => o,
                    Err(_) => return FFI_ERR_JSON,
                },
                Err(_) => return FFI_ERR_INVALID_UTF8,
            }
        } else {
            markdown::parser::ParseOptions::default()
        };
        let (ir, has_tables) = markdown::parser::markdown_to_ir_with_meta(md_str, &options);
        let has_code_blocks = ir
            .styles
            .iter()
            .any(|s| s.style == markdown::spans::MarkdownStyle::CodeBlock);
        #[derive(serde::Serialize)]
        struct IrOutput<'a> {
            text: &'a str,
            styles: &'a [markdown::spans::StyleSpan],
            links: &'a [markdown::spans::LinkSpan],
            has_code_blocks: bool,
            has_tables: bool,
        }
        let output = IrOutput {
            text: &ir.text,
            styles: &ir.styles,
            links: &ir.links,
            has_code_blocks,
            has_tables,
        };
        ffi_write_json(&output, out_slice)
    })
}

/// C FFI: Detect fenced code blocks in text.
/// Writes JSON array of fence block objects to `out_ptr`.
/// Returns bytes written, or negative on error.
///
/// # Safety
/// `text_ptr` must be valid UTF-8. `out_ptr` must be writable.
#[no_mangle]
pub unsafe extern "C" fn deneb_markdown_detect_fences(
    text_ptr: *const u8,
    text_len: usize,
    out_ptr: *mut u8,
    out_len: usize,
) -> i32 {
    if out_ptr.is_null() {
        return FFI_ERR_NULL_PTR;
    }
    let str_result = ffi_read_str(text_ptr, text_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_PANIC, move || {
        let text_str = match str_result {
            Ok(s) => s,
            Err(e) => return e,
        };
        let fences = markdown::fences::parse_fence_spans(text_str);
        ffi_write_json(&fences, out_slice)
    })
}
