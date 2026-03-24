/// Cosine similarity between two f64 vectors.
/// Returns 0.0 for empty or zero-norm vectors.
pub fn cosine_similarity(a: &[f64], b: &[f64]) -> f64 {
    if a.is_empty() || b.is_empty() {
        return 0.0;
    }
    let len = a.len().min(b.len());

    #[cfg(target_arch = "x86_64")]
    {
        // SSE2 is always available on x86_64.
        // Process 2 f64 elements at a time via 128-bit registers.
        cosine_similarity_sse2(&a[..len], &b[..len])
    }

    #[cfg(not(target_arch = "x86_64"))]
    {
        cosine_similarity_scalar(&a[..len], &b[..len])
    }
}

#[cfg(target_arch = "x86_64")]
fn cosine_similarity_sse2(a: &[f64], b: &[f64]) -> f64 {
    use std::arch::x86_64::*;

    let len = a.len();
    let chunks = len / 2;
    let remainder = len % 2;

    unsafe {
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

        // Horizontal sum of 2-wide vectors
        let dot = horizontal_sum_pd(dot_vec);
        let mut norm_a = horizontal_sum_pd(norm_a_vec);
        let mut norm_b = horizontal_sum_pd(norm_b_vec);

        // Handle remainder
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

#[cfg(target_arch = "x86_64")]
#[inline]
unsafe fn horizontal_sum_pd(v: std::arch::x86_64::__m128d) -> f64 {
    use std::arch::x86_64::*;
    let high = _mm_unpackhi_pd(v, v);
    let sum = _mm_add_sd(v, high);
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
}
