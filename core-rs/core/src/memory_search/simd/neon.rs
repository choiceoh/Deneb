//! NEON accumulator for aarch64 (DGX Spark Grace CPU): two f64 lanes per iteration
//! with fused multiply-add for improved accuracy.

#![allow(unsafe_code)]

/// Compute `(dot_product, norm_a_sq, norm_b_sq)` using NEON 128-bit registers.
/// Uses `vfmaq_f64` (fused multiply-add) for both dot product and squared norms.
///
/// # Safety
/// NEON is always available on aarch64 — no runtime feature detection needed.
pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64) {
    use std::arch::aarch64::*;

    let len = a.len();
    let chunks = len / 2;
    let remainder = len % 2;

    // SAFETY: NEON is always available on aarch64. We process aligned pairs of f64.
    unsafe {
        let mut dot_vec = vdupq_n_f64(0.0);
        let mut norm_a_vec = vdupq_n_f64(0.0);
        let mut norm_b_vec = vdupq_n_f64(0.0);

        for i in 0..chunks {
            let offset = i * 2;
            let va = vld1q_f64(a.as_ptr().add(offset));
            let vb = vld1q_f64(b.as_ptr().add(offset));
            dot_vec = vfmaq_f64(dot_vec, va, vb);
            norm_a_vec = vfmaq_f64(norm_a_vec, va, va);
            norm_b_vec = vfmaq_f64(norm_b_vec, vb, vb);
        }

        // Reduce 2-wide NEON accumulators to scalar sums.
        let mut dot = vaddvq_f64(dot_vec);
        let mut norm_a = vaddvq_f64(norm_a_vec);
        let mut norm_b = vaddvq_f64(norm_b_vec);

        // Scalar tail for odd-length vectors.
        if remainder > 0 {
            let idx = chunks * 2;
            let av = a[idx];
            let bv = b[idx];
            dot += av * bv;
            norm_a += av * av;
            norm_b += bv * bv;
        }

        (dot, norm_a, norm_b)
    }
}
