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


func TestOOXML_XLSX(t *testing.T) {
	data := make([]byte, 0, 50)
	data = append(data, 0x50, 0x4B, 0x03, 0x04)       // ZIP header
	data = append(data, make([]byte, 26)...)          // local file header padding
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



// --- Additional formats not in noffi fallback ---














func TestFtypFallback(t *testing.T) {
	// Non-zero first byte but ftyp at offset 4
	data := []byte{
		0x01, 0x02, 0x03, 0x04, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm',
	}
	assertMIME(t, data, "video/mp4")
}


func assertMIME(t *testing.T, data []byte, expected string) {
	t.Helper()
	got := DetectMIME(data)
	if got != expected {
		t.Errorf("DetectMIME(%x) = %q, want %q", data, got, expected)
	}
}
