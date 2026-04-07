// Package htmlmd converts HTML to Markdown using a two-pass tokenize+emit
// architecture ported from the Rust core (core-rs/core/src/parsing/html_to_markdown/).
package htmlmd

import (
	"strconv"
	"strings"
)

// namedEntities maps HTML named entities to their Unicode characters.
// Covers the mandatory five plus common typography, symbols, currency,
// arrows, and math entities.
var namedEntities = [...]struct {
	name string
	ch   rune
}{
	{"&nbsp;", '\u00A0'},
	{"&amp;", '&'},
	{"&quot;", '"'},
	{"&lt;", '<'},
	{"&gt;", '>'},
	{"&#39;", '\''},
	{"&apos;", '\''},
	// Typography
	{"&mdash;", '\u2014'},
	{"&ndash;", '\u2013'},
	{"&hellip;", '\u2026'},
	{"&laquo;", '\u00AB'},
	{"&raquo;", '\u00BB'},
	{"&lsquo;", '\u2018'},
	{"&rsquo;", '\u2019'},
	{"&ldquo;", '\u201C'},
	{"&rdquo;", '\u201D'},
	{"&bull;", '\u2022'},
	{"&middot;", '\u00B7'},
	{"&ensp;", '\u2002'},
	{"&emsp;", '\u2003'},
	{"&thinsp;", '\u2009'},
	// Symbols
	{"&copy;", '\u00A9'},
	{"&reg;", '\u00AE'},
	{"&trade;", '\u2122'},
	{"&deg;", '\u00B0'},
	{"&plusmn;", '\u00B1'},
	{"&times;", '\u00D7'},
	{"&divide;", '\u00F7'},
	{"&micro;", '\u00B5'},
	// Currency
	{"&euro;", '\u20AC'},
	{"&pound;", '\u00A3'},
	{"&yen;", '\u00A5'},
	{"&cent;", '\u00A2'},
	// Arrows
	{"&larr;", '\u2190'},
	{"&rarr;", '\u2192'},
	{"&uarr;", '\u2191'},
	{"&darr;", '\u2193'},
	// Math
	{"&ne;", '\u2260'},
	{"&le;", '\u2264'},
	{"&ge;", '\u2265'},
	{"&infin;", '\u221E'},
	// Misc
	{"&para;", '\u00B6'},
	{"&sect;", '\u00A7'},
	{"&dagger;", '\u2020'},
	{"&loz;", '\u25CA'},
}

// tryDecodeEntity attempts to decode an HTML entity at input[pos].
// Returns the decoded rune and bytes consumed, or (-1, 0) on failure.
func tryDecodeEntity(input string, pos int) (ch rune, consumed int) {
	rest := input[pos:]
	if rest == "" {
		return -1, 0
	}

	// Bound the prefix we lowercase to 12 bytes (covers longest named entities).
	prefixEnd := 12
	if prefixEnd > len(rest) {
		prefixEnd = len(rest)
	}
	restLower := strings.ToLower(rest[:prefixEnd])

	// Named entity lookup.
	for i := range namedEntities {
		e := &namedEntities[i]
		if strings.HasPrefix(restLower, e.name) {
			return e.ch, len(e.name)
		}
	}

	// Hex numeric: &#xHH;
	if strings.HasPrefix(restLower, "&#x") {
		after := rest[3:]
		limit := 12
		if limit > len(after) {
			limit = len(after)
		}
		semi := strings.IndexByte(after[:limit], ';')
		if semi > 0 {
			code, err := strconv.ParseUint(after[:semi], 16, 32)
			if err == nil {
				if r := rune(code); r >= 0 && isValidCodePoint(r) { //nolint:gosec // G115 — code is bounded by ParseUint with bitSize 32
					return r, 3 + semi + 1
				}
			}
		}
		return -1, 0
	}

	// Decimal numeric: &#DDD;
	if strings.HasPrefix(restLower, "&#") {
		after := rest[2:]
		limit := 12
		if limit > len(after) {
			limit = len(after)
		}
		semi := strings.IndexByte(after[:limit], ';')
		if semi > 0 {
			code, err := strconv.ParseUint(after[:semi], 10, 32)
			if err == nil {
				if r := rune(code); r >= 0 && isValidCodePoint(r) { //nolint:gosec // G115 — code is bounded by ParseUint with bitSize 32
					return r, 2 + semi + 1
				}
			}
		}
	}

	return -1, 0
}

// isValidCodePoint checks if a rune is a valid Unicode code point
// that can be encoded (excludes surrogates and out-of-range values).
func isValidCodePoint(r rune) bool {
	return r >= 0 && r <= 0x10FFFF && (r < 0xD800 || r > 0xDFFF)
}
