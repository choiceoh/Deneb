package htmlmd

import "strings"

// normalizeWhitespace collapses whitespace runs in the emitter output:
//  1. Remove \r
//  2. Collapse trailing whitespace on lines ([ \t]+\n → \n)
//  3. Collapse 3+ consecutive newlines to 2
//  4. Collapse multiple spaces/tabs to single space
//  5. Trim leading/trailing whitespace
func normalizeWhitespace(input string) string {
	// Step 1: remove \r.
	var b strings.Builder
	b.Grow(len(input))
	for _, ch := range input {
		if ch != '\r' {
			b.WriteRune(ch)
		}
	}
	s := b.String()

	// Step 2: collapse trailing whitespace on lines.
	b.Reset()
	b.Grow(len(s))
	var trailingWS strings.Builder
	for _, ch := range s {
		switch ch {
		case ' ', '\t':
			trailingWS.WriteRune(ch)
		case '\n':
			trailingWS.Reset()
			b.WriteByte('\n')
		default:
			if trailingWS.Len() > 0 {
				b.WriteString(trailingWS.String())
				trailingWS.Reset()
			}
			b.WriteRune(ch)
		}
	}
	s = b.String()

	// Step 3: collapse 3+ newlines to 2.
	b.Reset()
	b.Grow(len(s))
	nlCount := 0
	for _, ch := range s {
		if ch == '\n' {
			nlCount++
			if nlCount <= 2 {
				b.WriteByte('\n')
			}
		} else {
			nlCount = 0
			b.WriteRune(ch)
		}
	}
	s = b.String()

	// Step 4: collapse multiple spaces/tabs to single space.
	b.Reset()
	b.Grow(len(s))
	prevSpace := false
	for _, ch := range s {
		if ch == ' ' || ch == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
		} else {
			prevSpace = false
			b.WriteRune(ch)
		}
	}

	return strings.TrimSpace(b.String())
}

// normalizeInline collapses whitespace and trims for inline content
// (link text, title, blockquote).
func normalizeInline(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, ch := range s {
		if ch == '\r' {
			continue
		}
		if ch == ' ' || ch == '\t' || ch == '\n' {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			prevSpace = true
		} else {
			prevSpace = false
			b.WriteRune(ch)
		}
	}
	result := b.String()
	return strings.TrimRight(result, " ")
}
