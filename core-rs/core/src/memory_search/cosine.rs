//! Cosine similarity computation for vector search.
//!
//! Measures the cosine of the angle between two vectors:
//! `cos(θ) = (A·B) / (‖A‖ × ‖B‖)`, yielding a value in [-1, 1].
//!
//! SIMD acceleration is provided by the sibling [`super::simd`] module, which
//! dispatches to NEON (aarch64) or scalar at compile time.

/// Cosine similarity between two f64 vectors.
/// Returns 0.0 for empty or zero-norm vectors.
/// Result is clamped to [-1.0, 1.0] to guard against float imprecision.
/// Returns 0.0 if the result is NaN (e.g., from NaN inputs).
pub fn cosine_similarity(a: &[f64], b: &[f64]) -> f64 {
    if a.is_empty() || b.is_empty() {
        return 0.0;
    }
    let len = a.len().min(b.len());
    let (dot, norm_a, norm_b) = super::simd::accumulate(&a[..len], &b[..len]);
    let raw = finish(dot, norm_a, norm_b);
    // Guard: NaN from bad inputs → 0.0; clamp to valid range
    if raw.is_nan() {
        0.0
    } else {
        raw.clamp(-1.0, 1.0)
    }
}

/// Final cosine computation: dot / √(`norm_a` × `norm_b`).
/// Returns 0.0 for zero-norm vectors to avoid division by zero.
///
/// Uses `√(norm_a × norm_b)` instead of `√norm_a × √norm_b` — one fewer sqrt call.
#[inline]
fn finish(dot: f64, norm_a: f64, norm_b: f64) -> f64 {
    if norm_a == 0.0 || norm_b == 0.0 {
        return 0.0;
    }
    dot / (norm_a * norm_b).sqrt()
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

    // --- Property-based tests (proptest) ---

    mod proptests {
        use super::*;
        use proptest::prelude::*;

        /// Naive scalar cosine similarity for cross-checking the SIMD implementation.
        fn naive_cosine(a: &[f64], b: &[f64]) -> f64 {
            let len = a.len().min(b.len());
            if len == 0 {
                return 0.0;
            }
            let mut dot = 0.0_f64;
            let mut na = 0.0_f64;
            let mut nb = 0.0_f64;
            for i in 0..len {
                dot += a[i] * b[i];
                na += a[i] * a[i];
                nb += b[i] * b[i];
            }
            if na == 0.0 || nb == 0.0 {
                return 0.0;
            }
            let raw = dot / (na.sqrt() * nb.sqrt());
            if raw.is_nan() {
                0.0
            } else {
                raw.clamp(-1.0, 1.0)
            }
        }

        /// Strategy for finite f64 vectors (no NaN/Inf to keep arithmetic deterministic).
        fn finite_vec(max_len: usize) -> impl Strategy<Value = Vec<f64>> {
            prop::collection::vec(-1e6_f64..1e6_f64, 1..=max_len)
        }

        proptest! {
            #[test]
            fn result_always_in_range(
                a in prop::collection::vec(-1e6_f64..1e6_f64, 0..128),
                b in prop::collection::vec(-1e6_f64..1e6_f64, 0..128),
            ) {
                let sim = cosine_similarity(&a, &b);
                prop_assert!((-1.0..=1.0).contains(&sim),
                    "result {} out of [-1, 1]", sim);
            }

            #[test]
            fn self_similarity_is_one(a in finite_vec(128)) {
                // Skip all-zero vectors (norm = 0 → returns 0.0).
                let norm: f64 = a.iter().map(|x| x * x).sum();
                if norm > 0.0 {
                    let sim = cosine_similarity(&a, &a);
                    prop_assert!((sim - 1.0).abs() < 1e-9,
                        "self-similarity = {}, expected ~1.0", sim);
                }
            }

            #[test]
            fn negated_similarity_is_minus_one(a in finite_vec(128)) {
                let norm: f64 = a.iter().map(|x| x * x).sum();
                if norm > 0.0 {
                    let neg_a: Vec<f64> = a.iter().map(|x| -x).collect();
                    let sim = cosine_similarity(&a, &neg_a);
                    prop_assert!((sim - (-1.0)).abs() < 1e-9,
                        "negated similarity = {}, expected ~-1.0", sim);
                }
            }

            #[test]
            fn simd_matches_naive(
                a in finite_vec(256),
                b in finite_vec(256),
            ) {
                let sim = cosine_similarity(&a, &b);
                let expected = naive_cosine(&a, &b);
                prop_assert!((sim - expected).abs() < 1e-9,
                    "SIMD result {} differs from naive {} by more than 1e-9", sim, expected);
            }

            #[test]
            fn empty_vectors_return_zero(
                a in prop::collection::vec(-1e6_f64..1e6_f64, 0..64),
            ) {
                prop_assert_eq!(cosine_similarity(&[], &a), 0.0);
                prop_assert_eq!(cosine_similarity(&a, &[]), 0.0);
                prop_assert_eq!(cosine_similarity(&[], &[]), 0.0);
            }
        }
    }
}
