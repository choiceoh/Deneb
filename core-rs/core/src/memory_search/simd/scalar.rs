//! Scalar fallback accumulator for architectures without SIMD support.

/// Compute `(dot_product, norm_a_sq, norm_b_sq)` using a plain scalar loop.
pub fn accumulate(a: &[f64], b: &[f64]) -> (f64, f64, f64) {
    debug_assert_eq!(
        a.len(),
        b.len(),
        "accumulate: slices must have equal length"
    );
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

#[cfg(test)]
mod tests {
    use super::*;

    const EPS: f64 = 1e-12;

    #[test]
    fn empty_slices_return_zeros() {
        let (dot, na, nb) = accumulate(&[], &[]);
        assert!((dot - 0.0).abs() < EPS);
        assert!((na - 0.0).abs() < EPS);
        assert!((nb - 0.0).abs() < EPS);
    }

    #[test]
    fn identical_unit_vector() {
        // [1, 0, 0] · [1, 0, 0] = 1; norm² = 1 each.
        let v = [1.0_f64, 0.0, 0.0];
        let (dot, na, nb) = accumulate(&v, &v);
        assert!((dot - 1.0).abs() < EPS);
        assert!((na - 1.0).abs() < EPS);
        assert!((nb - 1.0).abs() < EPS);
    }

    #[test]
    fn orthogonal_vectors_dot_zero() {
        let a = [1.0_f64, 0.0];
        let b = [0.0_f64, 1.0];
        let (dot, na, nb) = accumulate(&a, &b);
        assert!((dot - 0.0).abs() < EPS);
        assert!((na - 1.0).abs() < EPS);
        assert!((nb - 1.0).abs() < EPS);
    }

    #[test]
    fn known_values() {
        // a = [1, 2], b = [3, 4]
        // dot = 1*3 + 2*4 = 11
        // norm_a² = 1 + 4 = 5
        // norm_b² = 9 + 16 = 25
        let a = [1.0_f64, 2.0];
        let b = [3.0_f64, 4.0];
        let (dot, na, nb) = accumulate(&a, &b);
        assert!((dot - 11.0).abs() < EPS);
        assert!((na - 5.0).abs() < EPS);
        assert!((nb - 25.0).abs() < EPS);
    }

    #[test]
    fn negative_values() {
        let a = [-1.0_f64, 2.0];
        let b = [3.0_f64, -4.0];
        // dot = -3 + (-8) = -11
        // norm_a² = 1 + 4 = 5
        // norm_b² = 9 + 16 = 25
        let (dot, na, nb) = accumulate(&a, &b);
        assert!((dot - (-11.0)).abs() < EPS);
        assert!((na - 5.0).abs() < EPS);
        assert!((nb - 25.0).abs() < EPS);
    }

    #[test]
    fn scaled_vector_norms() {
        // a = [2, 2], b = [2, 2]
        // dot = 4 + 4 = 8, norm_a² = norm_b² = 8
        let v = [2.0_f64, 2.0];
        let (dot, na, nb) = accumulate(&v, &v);
        assert!((dot - 8.0).abs() < EPS);
        assert!((na - 8.0).abs() < EPS);
        assert!((nb - 8.0).abs() < EPS);
    }
}
