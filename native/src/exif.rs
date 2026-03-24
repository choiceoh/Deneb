use napi::bindgen_prelude::*;
use napi_derive::napi;
use std::io::Cursor;

/// Read EXIF orientation from a JPEG buffer.
/// Returns orientation value 1-8, or null if not found / not JPEG.
#[napi]
pub fn read_jpeg_exif_orientation(buffer: Buffer) -> Option<u32> {
    read_orientation(&buffer)
}

/// Pure Rust implementation (testable without napi linking).
fn read_orientation(data: &[u8]) -> Option<u32> {
    if data.len() < 2 || data[0] != 0xFF || data[1] != 0xD8 {
        return None;
    }

    let reader = exif::Reader::new();
    let exif_data = reader
        .read_from_container(&mut Cursor::new(data))
        .ok()?;

    let orientation =
        exif_data.get_field(exif::Tag::Orientation, exif::In::PRIMARY)?;
    let value = orientation.value.get_uint(0)?;

    if (1..=8).contains(&value) {
        Some(value)
    } else {
        None
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_non_jpeg_returns_none() {
        let buf = vec![0x89, 0x50, 0x4E, 0x47]; // PNG magic
        assert_eq!(read_orientation(&buf), None);
    }

    #[test]
    fn test_empty_returns_none() {
        assert_eq!(read_orientation(&[]), None);
    }

    #[test]
    fn test_minimal_jpeg_no_exif() {
        let buf = vec![0xFF, 0xD8, 0xFF, 0xD9];
        assert_eq!(read_orientation(&buf), None);
    }
}
