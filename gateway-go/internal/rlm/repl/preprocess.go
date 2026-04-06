package repl

import (
	"regexp"
	"strings"
)

// Preprocess transforms common Python idioms into Starlark-compatible code
// so that LLMs can write natural Python without hitting Starlark limitations.
//
// Transformations:
//  1. f-strings → %-formatting:  f"text {var}" → "text %s" % (var,)
//  2. import statements → removed (builtins are pre-injected)
//  3. Escaped braces {{ / }} → { / } in the output format string
func Preprocess(code string) string {
	code = convertFStrings(code)
	code = stripImports(code)
	return code
}

// fstringPattern matches f"..." or f'...' (not triple-quoted).
// It handles nested braces at one level, which covers most LLM-generated code.
var fstringPattern = regexp.MustCompile(`f(["'])`)

// convertFStrings converts Python f-strings to %-formatting.
// Handles f"...", f'...', and escaped braces {{/}}.
func convertFStrings(code string) string {
	var out strings.Builder
	out.Grow(len(code))
	i := 0
	for i < len(code) {
		// Look for f" or f'
		if i+2 <= len(code) && code[i] == 'f' && (code[i+1] == '"' || code[i+1] == '\'') {
			// Check this isn't part of a word (e.g., "if", "def")
			if i > 0 && isIdentChar(code[i-1]) {
				out.WriteByte(code[i])
				i++
				continue
			}
			quote := code[i+1]
			// Check for triple quotes
			tripleQuote := false
			if i+3 < len(code) && code[i+2] == quote && code[i+3] == quote {
				tripleQuote = true
			}

			converted, advance := parseFString(code[i:], quote, tripleQuote)
			out.WriteString(converted)
			i += advance
		} else {
			out.WriteByte(code[i])
			i++
		}
	}
	return out.String()
}

// parseFString parses a single f-string starting at code[0]=='f' and returns
// the %-formatted replacement and how many bytes were consumed.
func parseFString(code string, quote byte, triple bool) (string, int) {
	// Skip 'f' + opening quote(s)
	start := 1
	endQuote := string(quote)
	if triple {
		start = 4 // f"""
		endQuote = string([]byte{quote, quote, quote})
	} else {
		start = 2 // f"
	}

	var fmtStr strings.Builder
	var exprs []string
	i := start

	for i < len(code) {
		// Check for closing quote
		if triple {
			if i+2 < len(code) && code[i] == quote && code[i+1] == quote && code[i+2] == quote {
				i += 3
				goto done
			}
		} else {
			if code[i] == quote {
				i++
				goto done
			}
		}

		// Escaped braces
		if i+1 < len(code) && code[i] == '{' && code[i+1] == '{' {
			fmtStr.WriteByte('{')
			i += 2
			continue
		}
		if i+1 < len(code) && code[i] == '}' && code[i+1] == '}' {
			fmtStr.WriteByte('}')
			i += 2
			continue
		}

		// Expression in braces
		if code[i] == '{' {
			i++ // skip '{'
			exprStart := i
			depth := 1
			for i < len(code) && depth > 0 {
				switch code[i] {
				case '{':
					depth++
				case '}':
					depth--
				case quote:
					if !triple {
						// Hit end of string inside expression — bail
						goto bail
					}
				}
				if depth > 0 {
					i++
				}
			}
			if depth != 0 {
				goto bail
			}
			exprStr := code[exprStart:i]
			i++ // skip '}'

			// Check for format spec like {val:.2f}
			fmtSpec := "%s"
			if colonIdx := findFormatColon(exprStr); colonIdx >= 0 {
				spec := exprStr[colonIdx+1:]
				exprStr = exprStr[:colonIdx]
				fmtSpec = "%" + spec
			}

			fmtStr.WriteString(fmtSpec)
			exprs = append(exprs, strings.TrimSpace(exprStr))
			continue
		}

		// Backslash escape
		if code[i] == '\\' && i+1 < len(code) {
			fmtStr.WriteByte(code[i])
			fmtStr.WriteByte(code[i+1])
			i += 2
			continue
		}

		fmtStr.WriteByte(code[i])
		i++
	}

bail:
	// Could not parse — return original unchanged
	return code[:i], i

done:
	// Build the %-formatted string
	var result strings.Builder
	result.WriteByte(quote)
	if triple {
		result.WriteByte(quote)
		result.WriteByte(quote)
	}
	result.WriteString(fmtStr.String())
	result.WriteString(endQuote)

	if len(exprs) > 0 {
		result.WriteString(" % (")
		for j, e := range exprs {
			if j > 0 {
				result.WriteString(", ")
			}
			result.WriteString(e)
		}
		result.WriteString(",)")
	}

	return result.String(), i
}

// findFormatColon finds the colon separating expression from format spec,
// skipping colons inside nested brackets/parens (e.g., dict comprehensions).
func findFormatColon(expr string) int {
	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '[', '(':
			depth++
		case ']', ')':
			depth--
		case ':':
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// importPattern matches `import xxx` or `from xxx import yyy` at start of line.
var importPattern = regexp.MustCompile(`(?m)^(from\s+\S+\s+)?import\s+.+$`)

// stripImports removes Python import statements which are not needed
// in Starlark (all builtins are pre-injected).
func stripImports(code string) string {
	return importPattern.ReplaceAllStringFunc(code, func(line string) string {
		return "# (removed: builtins are pre-injected)"
	})
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
