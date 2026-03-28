//! NEON accumulator for aarch64 (DGX Spark Grace CPU): 2x-unrolled loop
//! processing four f64 per iteration with two independent FMA chains to
//! hide Neoverse V2 FMA latency (~5 cycles).

#![allow(unsafe_code)]

/// Compute `(dot_product, norm_a_sq, norm_b_sq)` using NEON 128-bit registers.
/// Two independent accumulator pairs are interleaved so that the CPU can
/// issue FMAs from chain 1 while chain 0's results are still in-flight.
///
/// # Safety
/// NEON is always available on aarch64 — no runtime feature detection needed.
pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64) {
    use std::arch::aarch64::*;

    let len = a.len();
    let chunks4 = len / 4; // number of 4-element (2-register) iterations
    let mid = chunks4 * 4; // first index after the unrolled body

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
            // First pair (chain 0)
            let va0 = vld1q_f64(a.as_ptr().add(off));
            let vb0 = vld1q_f64(b.as_ptr().add(off));
            dot0 = vfmaq_f64(dot0, va0, vb0);
            na0 = vfmaq_f64(na0, va0, va0);
            nb0 = vfmaq_f64(nb0, vb0, vb0);

            // Second pair (chain 1) — independent from chain 0, can issue in parallel
            let va1 = vld1q_f64(a.as_ptr().add(off + 2));
            let vb1 = vld1q_f64(b.as_ptr().add(off + 2));
            dot1 = vfmaq_f64(dot1, va1, vb1);
            na1 = vfmaq_f64(na1, va1, va1);
            nb1 = vfmaq_f64(nb1, vb1, vb1);
        }

        // Merge the two chains and reduce to scalars.
        let dot_merged = vaddq_f64(dot0, dot1);
        let na_merged = vaddq_f64(na0, na1);
        let nb_merged = vaddq_f64(nb0, nb1);
        let mut dot = vaddvq_f64(dot_merged);
        let mut norm_a = vaddvq_f64(na_merged);
        let mut norm_b = vaddvq_f64(nb_merged);

        // Scalar tail for remaining 1–3 elements.
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
