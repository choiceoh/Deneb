//! Cosine similarity computation for vector search.
//!
//! Measures the cosine of the angle between two vectors:
//! `cos(θ) = (A·B) / (‖A‖ × ‖B‖)`, yielding a value in [-1, 1].
//! On x86_64, uses SSE2 intrinsics to process two f64 lanes at once;
//! on other architectures, falls back to a scalar loop.

/// Cosine similarity between two f64 vectors.
/// Returns 0.0 for empty or zero-norm vectors.
/// Result is clamped to [-1.0, 1.0] to guard against float imprecision.
/// Returns 0.0 if the result is NaN (e.g., from NaN inputs).
pub fn cosine_similarity(a: &[f64], b: &[f64]) -> f64 {
    if a.is_empty() || b.is_empty() {
        return 0.0;
    }
    let len = a.len().min(b.len());

    #[cfg(target_arch = "x86_64")]
    let raw = cosine_similarity_sse2(&a[..len], &b[..len]);

    #[cfg(not(target_arch = "x86_64"))]
    let raw = cosine_similarity_scalar(&a[..len], &b[..len]);

    // Guard: NaN from bad inputs → 0.0; clamp to valid range
    if raw.is_nan() {
        0.0
    } else {
        raw.clamp(-1.0, 1.0)
    }
}

/// SSE2-accelerated cosine similarity: processes two f64 elements per iteration
/// using 128-bit SIMD registers. Accumulates dot product and squared norms in
/// parallel, then reduces via horizontal sum. A scalar tail handles odd-length vectors.
#[cfg(target_arch = "x86_64")]
fn cosine_similarity_sse2(a: &[f64], b: &[f64]) -> f64 {
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
        let dot = horizontal_sum_pd(dot_vec);
        let mut norm_a = horizontal_sum_pd(norm_a_vec);
        let mut norm_b = horizontal_sum_pd(norm_b_vec);

        // Scalar tail for odd-length vectors.
        if remainder > 0 {
            let idx = chunks * 2;
            let av = a[idx];
            let bv = b[idx];
            norm_a += av * av;
            norm_b += bv * bv;
            return finish(dot + av * bv, norm_a, norm_b);
        }

        finish(dot, norm_a, norm_b)
    }
}

/// Reduce a 2-wide f64 SIMD vector to a single scalar sum: [lo, hi] → lo + hi.
#[cfg(target_arch = "x86_64")]
#[inline]
unsafe fn horizontal_sum_pd(v: std::arch::x86_64::__m128d) -> f64 {
    use std::arch::x86_64::*;
    let high = _mm_unpackhi_pd(v, v); // broadcast high lane → [hi, hi]
    let sum = _mm_add_sd(v, high); // lo + hi in low lane
    _mm_cvtsd_f64(sum)
}

#[cfg(not(target_arch = "x86_64"))]
fn cosine_similarity_scalar(a: &[f64], b: &[f64]) -> f64 {
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
    finish(dot, norm_a, norm_b)
}

/// Final cosine computation: dot / (√norm_a × √norm_b).
/// Returns 0.0 for zero-norm vectors to avoid division by zero.
#[inline]
fn finish(dot: f64, norm_a: f64, norm_b: f64) -> f64 {
    if norm_a == 0.0 || norm_b == 0.0 {
        return 0.0;
    }
    dot / (norm_a.sqrt() * norm_b.sqrt())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_empty_vectors() {
        assert_eq!(cosine_similarity(&[], &[1.0, 2.0]), 0.0);
        assert_eq!(cosine_similarity(&[1.0], &[]), 0.0);
        assert_eq!(cosine_similarity(&[], &[]), 0.0);
    }

    #[test]
    fn test_zero_vectors() {
        assert_eq!(cosine_similarity(&[0.0, 0.0], &[1.0, 2.0]), 0.0);
        assert_eq!(cosine_similarity(&[1.0, 2.0], &[0.0, 0.0]), 0.0);
    }

    #[test]
    fn test_identical_vectors() {
        let v = vec![1.0, 2.0, 3.0];
        let sim = cosine_similarity(&v, &v);
        assert!((sim - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_orthogonal_vectors() {
        let a = vec![1.0, 0.0];
        let b = vec![0.0, 1.0];
        assert!(cosine_similarity(&a, &b).abs() < 1e-10);
    }

    #[test]
    fn test_opposite_vectors() {
        let a = vec![1.0, 2.0, 3.0];
        let b = vec![-1.0, -2.0, -3.0];
        let sim = cosine_similarity(&a, &b);
        assert!((sim - (-1.0)).abs() < 1e-10);
    }

    #[test]
    fn test_different_lengths_uses_min() {
        let a = vec![1.0, 0.0, 0.0];
        let b = vec![1.0, 0.0];
        let sim = cosine_similarity(&a, &b);
        assert!((sim - 1.0).abs() < 1e-10);
    }

    #[test]
    fn test_known_value() {
        // cos(45°) ≈ 0.7071
        let a = vec![1.0, 0.0];
        let b = vec![1.0, 1.0];
        let sim = cosine_similarity(&a, &b);
        assert!((sim - std::f64::consts::FRAC_1_SQRT_2).abs() < 1e-10);
    }

    #[test]
    fn test_large_vector() {
        // Test SIMD path with many elements
        let a: Vec<f64> = (0..1024).map(|i| (i as f64) * 0.01).collect();
        let b: Vec<f64> = (0..1024).map(|i| (i as f64) * 0.02).collect();
        let sim = cosine_similarity(&a, &b);
        // Both vectors point in the same direction (positive linear), so similarity should be ~1
        assert!(sim > 0.99);
    }

    #[test]
    fn test_odd_length_vector() {
        // Odd length to test remainder handling in SIMD
        let a = vec![1.0, 2.0, 3.0];
        let b = vec![4.0, 5.0, 6.0];
        let sim = cosine_similarity(&a, &b);
        // Manual: dot=32, normA=14, normB=77 => 32/sqrt(14*77) ≈ 0.9746
        let expected = 32.0 / (14.0_f64.sqrt() * 77.0_f64.sqrt());
        assert!((sim - expected).abs() < 1e-10);
    }

    #[test]
    fn test_nan_input() {
        assert_eq!(cosine_similarity(&[f64::NAN, 1.0], &[1.0, 2.0]), 0.0);
        assert_eq!(cosine_similarity(&[1.0, 2.0], &[f64::NAN, 1.0]), 0.0);
    }

    #[test]
    fn test_infinity_input() {
        // Infinity inputs should not panic; result clamped or 0.0
        let sim = cosine_similarity(&[f64::INFINITY, 1.0], &[1.0, 2.0]);
        assert!(sim.is_finite());
        assert!((-1.0..=1.0).contains(&sim));
    }

    #[test]
    fn test_result_clamped() {
        // Identical vectors: result should be exactly 1.0, not 1.0000000000002
        let v = vec![1.0, 2.0, 3.0, 4.0, 5.0];
        let sim = cosine_similarity(&v, &v);
        assert_eq!(sim, 1.0);
        assert!(sim <= 1.0);
    }

    #[test]
    fn test_single_element() {
        assert!((cosine_similarity(&[3.0], &[3.0]) - 1.0).abs() < 1e-10);
        assert!((cosine_similarity(&[3.0], &[-3.0]) - (-1.0)).abs() < 1e-10);
    }
}
