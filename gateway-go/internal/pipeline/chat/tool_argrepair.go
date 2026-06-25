package chat

import (
	"bytes"
	"encoding/json"
	"strings"
)

// repairToolArguments fixes the common malformed-JSON patterns that open-weight
// models — notably DeepSeek V4 Flash, Deneb's main chat model — emit for tool
// call arguments: a surrounding markdown code fence, Python literals
// (None/True/False), and trailing commas.
//
// It is deliberately conservative, with two invariants that bound the risk:
//
//  1. If the input is ALREADY valid JSON it is returned untouched — valid calls
//     carry zero risk of corruption.
//  2. A repair is adopted ONLY when it turns invalid JSON into valid JSON.
//     Otherwise the original bytes pass through unchanged, preserving Deneb's
//     fail-fast behavior: the tool's own parse error stays the model's feedback
//     signal, and the loop detector (tool_loop.go) remains the backstop for
//     genuine tool confusion.
//
// Rationale: "open model bad at tool calling" is largely a harness problem. A
// model that emits ```json{…}``` or a trailing comma otherwise burns turns (and
// trips the loop detector) re-emitting a call that needs only a deterministic
// syntactic fix; repairing it lets the call land on the first attempt. Schema-
// aware repairs (null-for-optional, type coercion) are intentionally out of
// scope — they need per-tool schema plumbing, so measure the repair-Warn rate
// first before adding them.
func repairToolArguments(input json.RawMessage) (json.RawMessage, bool) {
	if len(bytes.TrimSpace(input)) == 0 || json.Valid(input) {
		return input, false
	}
	repaired := stripCodeFence(input)
	repaired = normalizePythonLiterals(repaired)
	repaired = stripTrailingCommas(repaired)
	if !bytes.Equal(repaired, input) && json.Valid(repaired) {
		return repaired, true
	}
	return input, false
}

// stripCodeFence removes a surrounding markdown code fence (```json … ``` or
// ``` … ```) that some models wrap tool arguments in. It strips leading/trailing
// backtick runs and an optional language tag; the caller re-validates, so an
// imperfect strip simply falls back to the original.
func stripCodeFence(b json.RawMessage) json.RawMessage {
	s := strings.TrimSpace(string(b))
	if !strings.HasPrefix(s, "```") {
		return b
	}
	s = strings.TrimSpace(strings.Trim(s, "`"))
	for _, lang := range []string{"json", "JSON"} {
		if strings.HasPrefix(s, lang) {
			if rest := strings.TrimSpace(s[len(lang):]); strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "[") {
				s = rest
			}
			break
		}
	}
	return json.RawMessage(strings.TrimSpace(s))
}

// normalizePythonLiterals rewrites bare Python literals None/True/False to their
// JSON equivalents null/true/false, but only outside of string literals so a
// value like "None reported" is left intact. Word-boundary checks avoid touching
// identifiers like "NoneType".
func normalizePythonLiterals(b json.RawMessage) json.RawMessage {
	s := string(b)
	if !strings.Contains(s, "None") && !strings.Contains(s, "True") && !strings.Contains(s, "False") {
		return b
	}
	var out strings.Builder
	out.Grow(len(s))
	inStr := false
	for i := 0; i < len(s); {
		c := s[i]
		if inStr {
			out.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				out.WriteByte(s[i+1])
				i += 2
				continue
			}
			if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		if c == '"' {
			inStr = true
			out.WriteByte(c)
			i++
			continue
		}
		if repl, n, ok := matchPyLiteral(s, i); ok {
			out.WriteString(repl)
			i += n
			continue
		}
		out.WriteByte(c)
		i++
	}
	return json.RawMessage(out.String())
}

// matchPyLiteral reports whether a Python literal starts at s[i] on a word
// boundary, returning its JSON replacement and the matched length.
func matchPyLiteral(s string, i int) (replacement string, n int, ok bool) {
	for _, lit := range []struct{ py, js string }{
		{"None", "null"},
		{"True", "true"},
		{"False", "false"},
	} {
		if !strings.HasPrefix(s[i:], lit.py) {
			continue
		}
		end := i + len(lit.py)
		if i > 0 && isIdentByte(s[i-1]) {
			continue
		}
		if end < len(s) && isIdentByte(s[end]) {
			continue
		}
		return lit.js, len(lit.py), true
	}
	return "", 0, false
}

func isIdentByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// stripTrailingCommas drops a comma that is followed (after optional whitespace)
// by a closing } or ], outside of string literals. Trailing commas are valid in
// Python/JS object and array literals but reject in strict JSON.
func stripTrailingCommas(b json.RawMessage) json.RawMessage {
	s := string(b)
	if !strings.Contains(s, ",") {
		return b
	}
	out := make([]byte, 0, len(s))
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			out = append(out, c)
			if c == '\\' && i+1 < len(s) {
				out = append(out, s[i+1])
				i++
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // drop the trailing comma
			}
		}
		out = append(out, c)
	}
	return json.RawMessage(out)
}
