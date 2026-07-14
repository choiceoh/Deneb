package coremedia

import "testing"

// Tests ported from core-rs/core/src/media/mod.rs

func TestDetectMIMEReturnsPNGForMagicBytes(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00}
	assertMIME(t, data, "image/png")
}

func TestDetectMIMEReturnsJPEGForMagicBytes(t *testing.T) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	assertMIME(t, data, "image/jpeg")
}

func TestDetectMIMEReturnsMP4ForFtypIsomBrand(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm',
	}
	assertMIME(t, data, "video/mp4")
}

func TestDetectMIMEReturnsJSONForBraceContent(t *testing.T) {
	assertMIME(t, []byte(`{"key":"value"}`), "application/json")
}

func TestDetectMIMEReturnsAVIFForFtypAvifBrand(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f',
	}
	assertMIME(t, data, "image/avif")
}

func TestDetectMIMEReturnsHEICForHeicAndMif1Brands(t *testing.T) {
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

func TestDetectMIMEReturnsXLSXForWorkbookMarker(t *testing.T) {
	data := make([]byte, 0, 50)
	data = append(data, 0x50, 0x4B, 0x03, 0x04)       // ZIP header
	data = append(data, make([]byte, 26)...)          // local file header padding
	data = append(data, []byte("xl/workbook.xml")...) // XLSX marker
	assertMIME(t, data, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
}

func TestDetectMIMEReturnsDOCXForDocumentMarker(t *testing.T) {
	data := make([]byte, 0, 50)
	data = append(data, 0x50, 0x4B, 0x03, 0x04)
	data = append(data, make([]byte, 26)...)
	data = append(data, []byte("word/document.xml")...)
	assertMIME(t, data, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
}

func TestDetectMIMEReturnsPPTXForPresentationMarker(t *testing.T) {
	data := make([]byte, 0, 50)
	data = append(data, 0x50, 0x4B, 0x03, 0x04)
	data = append(data, make([]byte, 26)...)
	data = append(data, []byte("ppt/presentation.xml")...)
	assertMIME(t, data, "application/vnd.openxmlformats-officedocument.presentationml.presentation")
}

func TestDetectMIMEReturnsZIPWithoutOfficeMarker(t *testing.T) {
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

func TestDetectMIMEReturnsCorrectMIMEForEveryByteFamily(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"mpeg frame sync", []byte{0xFF, 0xF3, 0x80, 0x00}, "audio/mpeg"},
		{"gif87a", []byte("GIF87a"), "image/gif"},
		{"webp riff", append([]byte("RIFF0000"), []byte("WEBP")...), "image/webp"},
		{"wave riff", append([]byte("RIFF0000"), []byte("WAVE")...), "audio/wav"},
		{"icon", []byte{0x00, 0x00, 0x01, 0x00}, "image/x-icon"},
		{"bitmap", []byte("BM00"), "image/bmp"},
		{"id3 audio", []byte("ID3\x04"), "audio/mpeg"},
		{"little endian tiff", []byte{'I', 'I', 0x2A, 0x00}, "image/tiff"},
		{"big endian tiff", []byte{'M', 'M', 0x00, 0x2A}, "image/tiff"},
		{"ogg", []byte("OggS"), "audio/ogg"},
		{"flac", []byte("fLaC"), "audio/flac"},
		{"webm", []byte{0x1A, 0x45, 0xDF, 0xA3}, "video/webm"},
		{"pdf", []byte("%PDF"), "application/pdf"},
		{"gzip", []byte{0x1F, 0x8B, 0x08, 0x00}, "application/gzip"},
		{"json array", []byte("[1] "), "application/json"},
		{"xml declaration", []byte("<?xml version=\"1.0\"?>"), "application/xml"},
		{"svg", []byte("<svg></svg>"), "application/xml"},
		{"html doctype", []byte("<!DOCTYPE html>"), "text/html"},
		{"uppercase html", []byte("<HTML></HTML>"), "text/html"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertMIME(t, tt.data, tt.want)
		})
	}
}

func TestDetectMIMEReturnsOctetStreamForTruncatedOrInvalidFamilies(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		[]byte("GIF8"),
		[]byte("RIFF-not-wave"),
		{0x89, 'N', 'O', 'T'},
		[]byte("<not-supported>"),
	} {
		assertMIME(t, data, "application/octet-stream")
	}
}

var detectedMIME string

func TestDetectMIMERepresentativePathsRunWithoutAllocations(t *testing.T) {
	for _, data := range [][]byte{
		{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
		[]byte("RIFF0000WEBP"),
		{0x00, 0x00, 0x00, 0x1C, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
		[]byte("unknown payload"),
	} {
		if allocations := testing.AllocsPerRun(1000, func() {
			detectedMIME = DetectMIME(data)
		}); allocations != 0 {
			t.Fatalf("DetectMIME(%x) allocations = %v, want 0", data, allocations)
		}
	}
}

func assertMIME(t *testing.T, data []byte, expected string) {
	t.Helper()
	got := DetectMIME(data)
	if got != expected {
		t.Errorf("DetectMIME(%x) = %q, want %q", data, got, expected)
	}
}
