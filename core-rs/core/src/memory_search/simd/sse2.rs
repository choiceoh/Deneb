//! SSE2 accumulator for x86_64: processes two f64 lanes per iteration
//! using 128-bit SIMD registers.

#![allow(unsafe_code)]

/// Reduce a 2-wide f64 SIMD vector to a single scalar sum: [lo, hi] → lo + hi.
#[inline]
unsafe fn horizontal_sum_pd(v: std::arch::x86_64::__m128d) -> f64 {
    use std::arch::x86_64::*;
    let high = _mm_unpackhi_pd(v, v); // broadcast high lane → [hi, hi]
    let sum = _mm_add_sd(v, high); // lo + hi in low lane
    _mm_cvtsd_f64(sum)
}

/// Compute `(dot_product, norm_a_sq, norm_b_sq)` using SSE2 128-bit registers.
/// Accumulates two f64 elements per iteration; scalar tail handles odd lengths.
pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64) {
    use std::arch::x86_64::*;

    let len = a.len();
    let chunks = len / 2;
    let remainder = len % 2;

    unsafe {
        // Accumulators: each __m128d holds two f64 partial sums.
        let mut dot_vec = _mm_setzero_pd();
        let mut norm_a_vec = _mm_setzero_pd();
        let mut norm_b_vec = _mm_setzero_pd();

        for i in 0..chunks {
            let offset = i * 2;
            let va = _mm_loadu_pd(a.as_ptr().add(offset));
            let vb = _mm_loadu_pd(b.as_ptr().add(offset));
            dot_vec = _mm_add_pd(dot_vec, _mm_mul_pd(va, vb));
            norm_a_vec = _mm_add_pd(norm_a_vec, _mm_mul_pd(va, va));
            norm_b_vec = _mm_add_pd(norm_b_vec, _mm_mul_pd(vb, vb));
        }

        // Reduce 2-wide SIMD accumulators to scalar sums.
        let mut dot = horizontal_sum_pd(dot_vec);
        let mut norm_a = horizontal_sum_pd(norm_a_vec);
        let mut norm_b = horizontal_sum_pd(norm_b_vec);

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
