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
