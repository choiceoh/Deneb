//! Scalar fallback accumulator for architectures without SIMD support.

/// Compute `(dot_product, norm_a_sq, norm_b_sq)` using a plain scalar loop.
pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64) {
    let mut dot = 0.0;
    let mut norm_a = 0.0;
    let mut norm_b = 0.0;
    for i in 0..a.len() {
        let av = a[i];
        let bv = b[i];
        dot += av * bv;
        norm_a += av * av;
        norm_b += bv * bv;
    }
    (dot, norm_a, norm_b)
}
