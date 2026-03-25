// EXIF orientation is exported by deneb-core (core-rs/core/src/exif.rs)
// via #[cfg_attr(feature = "napi_binding", napi)].
// This module is intentionally empty to avoid duplicate napi exports.

#[cfg(test)]
mod tests {
    #[test]
    fn test_exif_available_via_deneb_core() {
        // Smoke test: deneb_core::exif module exists and is accessible.
        let result = deneb_core::exif::read_jpeg_exif_orientation_impl(&[]);
        assert_eq!(result, None);
    }
}
