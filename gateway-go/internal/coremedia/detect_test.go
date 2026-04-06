package coremedia

import "testing"

// Tests ported from core-rs/core/src/media/mod.rs

func TestPNG(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	assertMIME(t, data, "image/png")
}

func TestJPEG(t *testing.T) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	assertMIME(t, data, "image/jpeg")
}

func TestGIF(t *testing.T) {
	assertMIME(t, []byte("GIF89a..."), "image/gif")
	assertMIME(t, []byte("GIF87a..."), "image/gif")
}

func TestWebP(t *testing.T) {
	data := []byte("RIFF\x00\x00\x00\x00WEBP")
	assertMIME(t, data, "image/webp")
}

func TestPDF(t *testing.T) {
	assertMIME(t, []byte("%PDF-1.4"), "application/pdf")
}

func TestMP4(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm',
	}
	assertMIME(t, data, "video/mp4")
}

func TestJSON(t *testing.T) {
	assertMIME(t, []byte(`{"key":"value"}`), "application/json")
}

func TestAVIF(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f',
	}
	assertMIME(t, data, "image/avif")
}

func TestHEIC(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c',
	}
	assertMIME(t, data, "image/heic")

	// ftyp box with 'mif1' brand (HEIF)
	dataMIF1 := []byte{
		0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'm', 'i', 'f', '1',
	}
	assertMIME(t, dataMIF1, "image/heic")
}

func TestTIFF(t *testing.T) {
	// TIFF little-endian
	assertMIME(t, []byte{'I', 'I', 0x2A, 0x00, 0x08}, "image/tiff")
	// TIFF big-endian
	assertMIME(t, []byte{'M', 'M', 0x00, 0x2A, 0x00}, "image/tiff")
}

func TestOOXML_XLSX(t *testing.T) {
	data := make([]byte, 0, 50)
	data = append(data, 0x50, 0x4B, 0x03, 0x04)    // ZIP header
	data = append(data, make([]byte, 26)...)         // local file header padding
	data = append(data, []byte("xl/workbook.xml")...) // XLSX marker
	assertMIME(t, data, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
}

func TestOOXML_DOCX(t *testing.T) {
	data := make([]byte, 0, 50)
	data = append(data, 0x50, 0x4B, 0x03, 0x04)
	data = append(data, make([]byte, 26)...)
	data = append(data, []byte("word/document.xml")...)
	assertMIME(t, data, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
}

func TestOOXML_PPTX(t *testing.T) {
	data := make([]byte, 0, 50)
	data = append(data, 0x50, 0x4B, 0x03, 0x04)
	data = append(data, make([]byte, 26)...)
	data = append(data, []byte("ppt/presentation.xml")...)
	assertMIME(t, data, "application/vnd.openxmlformats-officedocument.presentationml.presentation")
}

func TestPlainZIP(t *testing.T) {
	data := make([]byte, 0, 50)
	data = append(data, 0x50, 0x4B, 0x03, 0x04)
	data = append(data, make([]byte, 26)...)
	data = append(data, []byte("some/other/file.txt")...)
	assertMIME(t, data, "application/zip")
}

func TestUnknown(t *testing.T) {
	assertMIME(t, []byte{0x00, 0x01, 0x02, 0x03}, "application/octet-stream")
}

func TestTooShort(t *testing.T) {
	assertMIME(t, []byte{0x00}, "application/octet-stream")
}

// --- Additional formats not in noffi fallback ---

func TestBMP(t *testing.T) {
	assertMIME(t, []byte("BM\x00\x00\x00\x00"), "image/bmp")
}

func TestICO(t *testing.T) {
	assertMIME(t, []byte{0x00, 0x00, 0x01, 0x00, 0x01}, "image/x-icon")
}

func TestWAV(t *testing.T) {
	assertMIME(t, []byte("RIFF\x00\x00\x00\x00WAVE"), "audio/wav")
}

func TestMP3FrameSync(t *testing.T) {
	// MPEG1 Layer 3 frame header
	assertMIME(t, []byte{0xFF, 0xFB, 0x90, 0x00, 0x00}, "audio/mpeg")
}

func TestMP3ID3(t *testing.T) {
	assertMIME(t, []byte("ID3\x04\x00"), "audio/mpeg")
}

func TestOgg(t *testing.T) {
	assertMIME(t, []byte("OggS\x00"), "audio/ogg")
}

func TestFLAC(t *testing.T) {
	assertMIME(t, []byte("fLaC\x00"), "audio/flac")
}

func TestM4A(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' ',
	}
	assertMIME(t, data, "audio/mp4")
}

func TestWebM(t *testing.T) {
	assertMIME(t, []byte{0x1A, 0x45, 0xDF, 0xA3, 0x01}, "video/webm")
}

func TestGZIP(t *testing.T) {
	assertMIME(t, []byte{0x1F, 0x8B, 0x08, 0x00, 0x00}, "application/gzip")
}

func TestSVG(t *testing.T) {
	assertMIME(t, []byte("<svg xmlns="), "application/xml")
}

func TestHTML(t *testing.T) {
	assertMIME(t, []byte("<html>"), "text/html")
	assertMIME(t, []byte("<HTML>"), "text/html")
	assertMIME(t, []byte("<!DOCTYPE html>"), "text/html")
}

func TestXML(t *testing.T) {
	assertMIME(t, []byte("<?xml version=\"1.0\"?>"), "application/xml")
}

func TestFtypFallback(t *testing.T) {
	// Non-zero first byte but ftyp at offset 4
	data := []byte{
		0x01, 0x02, 0x03, 0x04, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm',
	}
	assertMIME(t, data, "video/mp4")
}

func TestJSONArray(t *testing.T) {
	assertMIME(t, []byte(`[1,2,3]`), "application/json")
}

func assertMIME(t *testing.T, data []byte, expected string) {
	t.Helper()
	got := DetectMIME(data)
	if got != expected {
		t.Errorf("DetectMIME(%x) = %q, want %q", data, got, expected)
	}
}
