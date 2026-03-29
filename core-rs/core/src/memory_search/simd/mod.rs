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

#[cfg(test)]
mod tests {
    use super::*;

    const EPS: f64 = 1e-10;

    #[test]
    fn dispatcher_empty() {
        let (dot, na, nb) = accumulate(&[], &[]);
        assert!((dot).abs() < EPS);
        assert!((na).abs() < EPS);
        assert!((nb).abs() < EPS);
    }

    #[test]
    fn dispatcher_matches_scalar_known_values() {
        let a = [1.0_f64, 2.0, 3.0];
        let b = [4.0_f64, 5.0, 6.0];
        // dot = 4 + 10 + 18 = 32; norm_a² = 14; norm_b² = 77
        let (dot, na, nb) = accumulate(&a, &b);
        let (dot_s, na_s, nb_s) = scalar::accumulate(&a, &b);
        assert!((dot - dot_s).abs() < EPS, "dot mismatch: {dot} vs {dot_s}");
        assert!((na - na_s).abs() < EPS, "norm_a mismatch");
        assert!((nb - nb_s).abs() < EPS, "norm_b mismatch");
    }

    #[test]
    fn dispatcher_orthogonal() {
        let a = [1.0_f64, 0.0, 0.0];
        let b = [0.0_f64, 1.0, 0.0];
        let (dot, na, nb) = accumulate(&a, &b);
        assert!(dot.abs() < EPS, "orthogonal dot must be ~0");
        assert!((na - 1.0).abs() < EPS);
        assert!((nb - 1.0).abs() < EPS);
    }

    #[test]
    fn dispatcher_larger_slice() {
        // Generates a 64-element slice to exercise any SIMD loop unrolling.
        let a: Vec<f64> = (0..64).map(|i| i as f64).collect();
        let b: Vec<f64> = (0..64).map(|i| i as f64).collect();
        let (dot, na, nb) = accumulate(&a, &b);
        let (dot_s, na_s, nb_s) = scalar::accumulate(&a, &b);
        assert!((dot - dot_s).abs() < EPS);
        assert!((na - na_s).abs() < EPS);
        assert!((nb - nb_s).abs() < EPS);
    }
}
