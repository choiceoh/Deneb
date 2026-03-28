//! SIMD backends for accumulating dot-product and squared norms over f64 slices.
//!
//! Each backend exposes the same function:
//! ```text
//! pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64)
//! ```
//! returning `(dot_product, norm_a_sq, norm_b_sq)`.
//!
//! ## Adding a new architecture
//! 1. Create `<arch>.rs` implementing `pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64)`.
//! 2. Add `#[cfg(target_arch = "<arch>")] pub mod <arch>;` below.
//! 3. Add the matching `#[cfg(target_arch = "<arch>")] let result = <arch>::accumulate(a, b);` arm
//!    in [`accumulate`].

pub mod scalar;

#[cfg(target_arch = "x86_64")]
pub mod sse2;

#[cfg(target_arch = "aarch64")]
pub mod neon;

/// Compute `(dot_product, norm_a_sq, norm_b_sq)` over equal-length f64 slices,
/// dispatching to the fastest SIMD backend available at compile time:
/// SSE2 on x86_64, NEON on aarch64, scalar otherwise.
#[inline]
pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64) {
    #[cfg(target_arch = "x86_64")]
    let result = sse2::accumulate(a, b);

    #[cfg(target_arch = "aarch64")]
    let result = neon::accumulate(a, b);

    #[cfg(not(any(target_arch = "x86_64", target_arch = "aarch64")))]
    let result = scalar::accumulate(a, b);

    result
}
