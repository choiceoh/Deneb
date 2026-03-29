//! C FFI: Markdown parsing to IR and fenced code block detection.

use crate::ffi_utils::*;

/// C FFI: Parse markdown text into a `MarkdownIR` structure.
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
        return FFI_ERR_NULL_POINTER;
    }
    if md_len > FFI_MAX_INPUT_LEN {
        return FFI_ERR_INPUT_TOO_LARGE;
    }
    // SAFETY: md_ptr and out_ptr are null-checked above, md_len bounded by
    // FFI_MAX_INPUT_LEN. The Go caller guarantees both buffers are valid.
    let md_slice = std::slice::from_raw_parts(md_ptr, md_len);
    let out_slice = std::slice::from_raw_parts_mut(out_ptr, out_len);
    ffi_catch(FFI_ERR_RUST_PANIC, move || {
        let md_str = match std::str::from_utf8(md_slice) {
            Ok(s) => s,
            Err(_) => return FFI_ERR_INVALID_UTF8,
        };
        let options = if !opts_ptr.is_null() && opts_len > 0 {
            // SAFETY: opts_ptr is non-null (checked in this branch), opts_len > 0.
            let opts_bytes = std::slice::from_raw_parts(opts_ptr, opts_len);
            match std::str::from_utf8(opts_bytes) {
                Ok(s) => match serde_json::from_str::<crate::markdown::parser::ParseOptions>(s) {
                    Ok(o) => o,
                    Err(_) => return FFI_ERR_JSON_ERROR,
                },
                Err(_) => return FFI_ERR_INVALID_UTF8,
            }
        } else {
            crate::markdown::parser::ParseOptions::default()
        };
        let (ir, has_tables) = crate::markdown::parser::markdown_to_ir_with_meta(md_str, &options);
        let has_code_blocks = ir
            .styles
            .iter()
            .any(|s| s.style == crate::markdown::spans::MarkdownStyle::CodeBlock);
        #[derive(serde::Serialize)]
        struct IrOutput<'a> {
            text: &'a str,
            styles: &'a [crate::markdown::spans::StyleSpan],
            links: &'a [crate::markdown::spans::LinkSpan],
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
        ffi_write_json(out_slice, &output)
    })
}

ffi_string_to_buffer!(
    /// C FFI: Detect fenced code blocks in text.
    /// Writes JSON array of fence block objects to `out_ptr`.
    /// Returns bytes written, or negative on error.
    ///
    /// # Safety
    /// `text_ptr` must be valid UTF-8. `out_ptr` must be writable.
    fn deneb_markdown_detect_fences(
        text_ptr,
        text_len,
        out = (out_ptr, out_len),
        max_len = FFI_MAX_INPUT_LEN,
        text_str,
        out_slice
    ) {
        let fences = crate::markdown::fences::parse_fence_spans(text_str);
        ffi_write_json(out_slice, &fences)
    }
);
