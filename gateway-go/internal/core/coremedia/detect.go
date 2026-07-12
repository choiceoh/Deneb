// Package coremedia provides pure-Go media MIME detection.
//
// Port of core-rs/core/src/media/mod.rs — 21+ format magic-byte sniffing
// with ISOBMFF ftyp box parsing and ZIP OOXML detection.
package coremedia

import "bytes"

// DetectMIME detects the MIME type from the first bytes of data using
// magic byte sniffing. Uses first-byte dispatch to minimize comparisons.
// Returns "application/octet-stream" for unrecognized data.
//
// Zero allocation on all paths.
func DetectMIME(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}
	if mime := detectByLeadingByte(data); mime != "" {
		return mime
	}
	// Some MP4 variants use a non-zero box-size prefix, so their first byte
	// cannot participate in the fast dispatch above.
	if mime := detectFtyp(data); mime != "" {
		return mime
	}
	return "application/octet-stream"
}

// detectByLeadingByte owns only dispatch. Format-specific validation stays in
// small detectors below, which keeps adding a new format from increasing the
// reasoning cost of every existing branch.
func detectByLeadingByte(data []byte) string {
	switch data[0] {
	case 0x89:
		return detectPNG(data)
	case 0xFF:
		return detectFFMedia(data)
	case 'G':
		return detectGIF(data)
	case 'R':
		return detectRIFF(data)
	case 0x00:
		return detectZeroPrefixed(data)
	case 'B':
		return detectBMP(data)
	case 'I':
		return detectIFormats(data)
	case 'M':
		return detectBigEndianTIFF(data)
	case 'O':
		return detectOgg(data)
	case 'f':
		return detectFLACOrFtyp(data)
	case 0x1A:
		return detectWebM(data)
	case '%':
		return detectPDF(data)
	case 0x50:
		return detectZIP(data)
	case 0x1F:
		return detectGZIP(data)
	case '{', '[':
		return "application/json"
	case '<':
		return detectMarkup(data)
	}
	return ""
}

func detectPNG(data []byte) string {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")) {
		return "image/png"
	}
	return ""
}

func detectFFMedia(data []byte) string {
	if data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	// MP3 frame sync: the top 3 bits cover MPEG1, MPEG2, and MPEG2.5.
	if data[1]&0xE0 == 0xE0 {
		return "audio/mpeg"
	}
	return ""
}

func detectGIF(data []byte) string {
	if len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF89a")) || bytes.Equal(data[:6], []byte("GIF87a"))) {
		return "image/gif"
	}
	return ""
}

func detectRIFF(data []byte) string {
	if len(data) < 12 || !bytes.Equal(data[:4], []byte("RIFF")) {
		return ""
	}
	switch string(data[8:12]) {
	case "WEBP":
		return "image/webp"
	case "WAVE":
		return "audio/wav"
	default:
		return ""
	}
}

func detectZeroPrefixed(data []byte) string {
	if data[1] == 0x00 && data[2] == 0x01 && data[3] == 0x00 {
		return "image/x-icon"
	}
	return detectFtyp(data)
}

func detectBMP(data []byte) string {
	if data[1] == 'M' {
		return "image/bmp"
	}
	return ""
}

func detectIFormats(data []byte) string {
	if data[1] == 'D' && data[2] == '3' {
		return "audio/mpeg"
	}
	if data[1] == 'I' && data[2] == 0x2A && data[3] == 0x00 {
		return "image/tiff"
	}
	return ""
}

func detectBigEndianTIFF(data []byte) string {
	if data[1] == 'M' && data[2] == 0x00 && data[3] == 0x2A {
		return "image/tiff"
	}
	return ""
}

func detectOgg(data []byte) string {
	if bytes.Equal(data[:4], []byte("OggS")) {
		return "audio/ogg"
	}
	return ""
}

func detectFLACOrFtyp(data []byte) string {
	if bytes.Equal(data[:4], []byte("fLaC")) {
		return "audio/flac"
	}
	return detectFtyp(data)
}

func detectWebM(data []byte) string {
	if bytes.Equal(data[:4], []byte{0x1A, 0x45, 0xDF, 0xA3}) {
		return "video/webm"
	}
	return ""
}

func detectPDF(data []byte) string {
	if bytes.Equal(data[:4], []byte("%PDF")) {
		return "application/pdf"
	}
	return ""
}

func detectZIP(data []byte) string {
	if !bytes.Equal(data[:4], []byte("PK\x03\x04")) {
		return ""
	}
	if mime := detectOOXML(data); mime != "" {
		return mime
	}
	return "application/zip"
}

func detectGZIP(data []byte) string {
	if data[1] == 0x8B {
		return "application/gzip"
	}
	return ""
}

func detectMarkup(data []byte) string {
	if hasPrefixStr(data, "<?xml") || hasPrefixStr(data, "<svg") {
		return "application/xml"
	}
	if hasPrefixStr(data, "<!DOCTYPE") || hasPrefixStr(data, "<html") || hasPrefixStr(data, "<HTML") {
		return "text/html"
	}
	return ""
}

// detectFtyp detects ISO Base Media File Format variants from the ftyp box.
// The 4-byte brand at offset 8 distinguishes MP4, M4A, AVIF, HEIC/HEIF.
func detectFtyp(data []byte) string {
	if len(data) < 8 || string(data[4:8]) != "ftyp" {
		return ""
	}
	if len(data) >= 12 {
		brand := string(data[8:12])
		switch brand {
		case "M4A ", "M4B ":
			return "audio/mp4"
		case "avif", "avis":
			return "image/avif"
		case "heic", "heix", "hevc", "mif1":
			return "image/heic"
		}
	}
	return "video/mp4"
}

// detectOOXML scans the first 8KB of a ZIP file for OOXML path markers
// to distinguish XLSX, DOCX, and PPTX from plain ZIP.
func detectOOXML(data []byte) string {
	scanLen := len(data)
	if scanLen > 8192 {
		scanLen = 8192
	}
	window := data[:scanLen]

	if bytes.Contains(window, []byte("xl/workbook.xml")) ||
		bytes.Contains(window, []byte("xl/sharedStrings.xml")) ||
		bytes.Contains(window, []byte("xl/styles.xml")) {
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	if bytes.Contains(window, []byte("word/document.xml")) ||
		bytes.Contains(window, []byte("word/styles.xml")) ||
		bytes.Contains(window, []byte("word/settings.xml")) {
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	}
	if bytes.Contains(window, []byte("ppt/presentation.xml")) ||
		bytes.Contains(window, []byte("ppt/slides/")) ||
		bytes.Contains(window, []byte("ppt/slideMasters/")) {
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	}
	return ""
}

// hasPrefixStr checks if data starts with prefix string (avoids allocation).
func hasPrefixStr(data []byte, prefix string) bool {
	return len(data) >= len(prefix) && string(data[:len(prefix)]) == prefix
}
