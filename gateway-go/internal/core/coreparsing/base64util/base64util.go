// Package base64util provides base64 validation and size estimation.
//
// Ported from core-rs/core/src/parsing/base64_util.rs.
// Estimates decoded size without decoding, and canonicalizes base64 strings
// (strips whitespace, normalizes URL-safe chars, validates format).
package base64util

import "errors"

// Estimate estimates the number of decoded bytes from a base64 string without
// allocating or decoding. Whitespace characters (ASCII <= 0x20) are skipped;
// padding '=' is detected by scanning from the end.
func Estimate(input string) int64 {
	if input == "" {
		return 0
	}

	effectiveLen := 0
	for i := range len(input) {
		if input[i] > 0x20 {
			effectiveLen++
		}
	}
	if effectiveLen == 0 {
		return 0
	}

	// Find padding by scanning from the end, skipping whitespace.
	padding := 0
	end := len(input) - 1
	for end >= 0 && input[end] <= 0x20 {
		end--
	}
	if end >= 0 && input[end] == '=' {
		padding = 1
		end--
		for end >= 0 && input[end] <= 0x20 {
			end--
		}
		if end >= 0 && input[end] == '=' {
			padding = 2
		}
	}

	estimated := (effectiveLen * 3) / 4
	estimated -= padding
	if estimated < 0 {
		estimated = 0
	}
	return int64(estimated)
}

// ErrInvalidBase64 is returned when the input is not valid base64.
var ErrInvalidBase64 = errors.New("invalid base64")

// Canonicalize validates and canonicalizes a base64 string.
// Strips whitespace, normalizes URL-safe characters (- -> +, _ -> /),
// and validates that the result is non-empty, length is a multiple of 4,
// all characters are [A-Za-z0-9+/] with up to 2 trailing '='.
func Canonicalize(input string) (string, error) {
	if input == "" {
		return "", ErrInvalidBase64
	}

	// Strip whitespace and normalize URL-safe base64 chars.
	cleaned := make([]byte, 0, len(input))
	for i := range len(input) {
		b := input[i]
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v' {
			continue
		}
		// URL-safe base64 uses '-' for '+' and '_' for '/'.
		switch b {
		case '-':
			cleaned = append(cleaned, '+')
		case '_':
			cleaned = append(cleaned, '/')
		default:
			cleaned = append(cleaned, b)
		}
	}

	if len(cleaned) == 0 || len(cleaned)%4 != 0 {
		return "", ErrInvalidBase64
	}

	// Find where padding starts.
	dataEnd := len(cleaned)
	if dataEnd > 0 && cleaned[dataEnd-1] == '=' {
		dataEnd--
		if dataEnd > 0 && cleaned[dataEnd-1] == '=' {
			dataEnd--
		}
	}

	// Check that padding is at most 2.
	paddingCount := len(cleaned) - dataEnd
	if paddingCount > 2 {
		return "", ErrInvalidBase64
	}

	// Validate data characters.
	for _, b := range cleaned[:dataEnd] {
		if !isBase64Char(b) {
			return "", ErrInvalidBase64
		}
	}

	// Validate that padding chars are only '='.
	for _, b := range cleaned[dataEnd:] {
		if b != '=' {
			return "", ErrInvalidBase64
		}
	}

	return string(cleaned), nil
}

func isBase64Char(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '+' || b == '/'
}
