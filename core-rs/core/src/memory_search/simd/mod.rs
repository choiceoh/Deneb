//! SIMD-accelerated accumulator for dot-product and squared norms over f64 slices.
//!
//! On aarch64 (DGX Spark), uses NEON 128-bit registers with a 2x-unrolled loop
//! to hide Neoverse V2 FMA latency (~5 cycles). Scalar fallback on other
//! architectures (CI runners).

pub mod scalar;

/// Compute `(dot_product, norm_a_sq, norm_b_sq)` over equal-length f64 slices.
///
/// # aarch64 (production)
/// Two independent NEON FMA chains processing four f64 per iteration.
/// The CPU can issue FMAs from chain 1 while chain 0's results are still
/// in-flight, fully utilizing the Neoverse V2 pipeline.
///
/// # Other architectures (CI)
/// Plain scalar loop — correctness-equivalent, no SIMD dependency.
#[inline]
#[allow(unsafe_code)]
pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64) {
    debug_assert_eq!(a.len(), b.len(), "accumulate: slices must have equal length");

    #[cfg(target_arch = "aarch64")]
    {
        use std::arch::aarch64::*;

        let len = a.len();
        let chunks4 = len / 4;
        let mid = chunks4 * 4;

        // SAFETY: NEON is always available on aarch64.
        unsafe {
            // Two independent accumulator chains to hide FMA pipeline latency.
            let mut dot0 = vdupq_n_f64(0.0);
            let mut dot1 = vdupq_n_f64(0.0);
            let mut na0 = vdupq_n_f64(0.0);
            let mut na1 = vdupq_n_f64(0.0);
            let mut nb0 = vdupq_n_f64(0.0);
            let mut nb1 = vdupq_n_f64(0.0);

            for i in 0..chunks4 {
                let off = i * 4;
                // Chain 0
                let va0 = vld1q_f64(a.as_ptr().add(off));
                let vb0 = vld1q_f64(b.as_ptr().add(off));
                dot0 = vfmaq_f64(dot0, va0, vb0);
                na0 = vfmaq_f64(na0, va0, va0);
                nb0 = vfmaq_f64(nb0, vb0, vb0);

                // Chain 1 — independent, can issue in parallel
                let va1 = vld1q_f64(a.as_ptr().add(off + 2));
                let vb1 = vld1q_f64(b.as_ptr().add(off + 2));
                dot1 = vfmaq_f64(dot1, va1, vb1);
                na1 = vfmaq_f64(na1, va1, va1);
                nb1 = vfmaq_f64(nb1, vb1, vb1);
            }

            // Merge chains and reduce to scalars.
            let dot_merged = vaddq_f64(dot0, dot1);
            let na_merged = vaddq_f64(na0, na1);
            let nb_merged = vaddq_f64(nb0, nb1);
            let mut dot = vaddvq_f64(dot_merged);
            let mut norm_a = vaddvq_f64(na_merged);
            let mut norm_b = vaddvq_f64(nb_merged);

            // Scalar tail for remaining 1-3 elements.
            for j in mid..len {
                let av = a[j];
                let bv = b[j];
                dot += av * bv;
                norm_a += av * av;
                norm_b += bv * bv;
            }

            (dot, norm_a, norm_b)
        }
    }

    #[cfg(not(target_arch = "aarch64"))]
    {
        scalar::accumulate(a, b)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const EPS: f64 = 1e-10;

    #[test]
    fn empty() {
        let (dot, na, nb) = accumulate(&[], &[]);
        assert!((dot).abs() < EPS);
        assert!((na).abs() < EPS);
        assert!((nb).abs() < EPS);
    }

    #[test]
    fn matches_scalar_known_values() {
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
    fn orthogonal() {
        let a = [1.0_f64, 0.0, 0.0];
        let b = [0.0_f64, 1.0, 0.0];
        let (dot, na, nb) = accumulate(&a, &b);
        assert!(dot.abs() < EPS, "orthogonal dot must be ~0");
        assert!((na - 1.0).abs() < EPS);
        assert!((nb - 1.0).abs() < EPS);
    }

    #[test]
    fn larger_slice() {
        // 64-element slice to exercise SIMD loop unrolling.
        let a: Vec<f64> = (0..64).map(|i| i as f64).collect();
        let b: Vec<f64> = (0..64).map(|i| i as f64).collect();
        let (dot, na, nb) = accumulate(&a, &b);
        let (dot_s, na_s, nb_s) = scalar::accumulate(&a, &b);
        assert!((dot - dot_s).abs() < EPS);
        assert!((na - na_s).abs() < EPS);
        assert!((nb - nb_s).abs() < EPS);
    }
}
