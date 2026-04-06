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

	switch data[0] {
	case 0x89:
		if len(data) >= 8 &&
			data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
			data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A {
			return "image/png"
		}

	case 0xFF:
		if data[1] == 0xD8 && data[2] == 0xFF {
			return "image/jpeg"
		}
		// MP3 frame sync: 12-bit sync word 0xFFF or 0xFFE.
		// Top 3 bits of byte[1] must be set (0xE0 mask), covering MPEG1/2/2.5.
		if data[1]&0xE0 == 0xE0 {
			return "audio/mpeg"
		}

	case 'G':
		if len(data) >= 6 && data[1] == 'I' && data[2] == 'F' {
			if (data[3] == '8' && data[4] == '9' && data[5] == 'a') ||
				(data[3] == '8' && data[4] == '7' && data[5] == 'a') {
				return "image/gif"
			}
		}

	case 'R':
		if len(data) >= 12 && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' {
			fourCC := string(data[8:12])
			if fourCC == "WEBP" {
				return "image/webp"
			}
			if fourCC == "WAVE" {
				return "audio/wav"
			}
		}

	case 0x00:
		if data[1] == 0x00 && data[2] == 0x01 && data[3] == 0x00 {
			return "image/x-icon"
		}
		if mime := detectFtyp(data); mime != "" {
			return mime
		}

	case 'B':
		if data[1] == 'M' {
			return "image/bmp"
		}

	case 'I':
		if len(data) >= 3 && data[1] == 'D' && data[2] == '3' {
			return "audio/mpeg"
		}
		// TIFF little-endian: II\x2A\x00
		if data[1] == 'I' && data[2] == 0x2A && data[3] == 0x00 {
			return "image/tiff"
		}

	case 'M':
		// TIFF big-endian: MM\x00\x2A
		if data[1] == 'M' && data[2] == 0x00 && data[3] == 0x2A {
			return "image/tiff"
		}

	case 'O':
		if len(data) >= 4 && data[1] == 'g' && data[2] == 'g' && data[3] == 'S' {
			return "audio/ogg"
		}

	case 'f':
		if len(data) >= 4 && data[1] == 'L' && data[2] == 'a' && data[3] == 'C' {
			return "audio/flac"
		}
		if mime := detectFtyp(data); mime != "" {
			return mime
		}

	case 0x1A:
		if data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 {
			return "video/webm"
		}

	case '%':
		if data[1] == 'P' && data[2] == 'D' && data[3] == 'F' {
			return "application/pdf"
		}

	case 0x50:
		if data[1] == 0x4B && data[2] == 0x03 && data[3] == 0x04 {
			if ooxml := detectOOXML(data); ooxml != "" {
				return ooxml
			}
			return "application/zip"
		}

	case 0x1F:
		if data[1] == 0x8B {
			return "application/gzip"
		}

	case '{', '[':
		return "application/json"

	case '<':
		if hasPrefixStr(data, "<?xml") || hasPrefixStr(data, "<svg") {
			return "application/xml"
		}
		if hasPrefixStr(data, "<!DOCTYPE") || hasPrefixStr(data, "<html") || hasPrefixStr(data, "<HTML") {
			return "text/html"
		}
	}

	// Fallback: check ftyp at offset 4 for non-zero first byte MP4 variants.
	if mime := detectFtyp(data); mime != "" {
		return mime
	}

	return "application/octet-stream"
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
