// CRC32 and PNG encoding are exported by deneb-core (core-rs/core/src/png.rs)
// via #[cfg_attr(feature = "napi_binding", napi)].
// This module is intentionally empty to avoid duplicate napi exports.

#[cfg(test)]
mod tests {
    #[test]
    fn test_crc32_available_via_deneb_core() {
        // Smoke test: deneb_core::png module exists and is accessible.
        assert_eq!(deneb_core::png::crc32_impl(&[]), 0);
    }

    #[test]
    fn test_crc32_known_value_via_deneb_core() {
        assert_eq!(deneb_core::png::crc32_impl(b"IEND"), 0xAE42_6082);
    }
}
