package autoresearch

import (
	"regexp"
	"strings"
)

// --- LLM response parsing ---

// parseLLMResponse extracts hypothesis and file changes from the LLM output.
func parseLLMResponse(resp string, targetFiles []string) (string, map[string]string) {
	var hypothesis string
	changes := make(map[string]string)

	// Extract hypothesis.
	lines := strings.SplitN(resp, "\n", 2)
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "HYPOTHESIS:") {
		hypothesis = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "HYPOTHESIS:"))
	}

	// Extract file contents between --- FILE: <name> --- and --- END FILE ---.
	filePattern := regexp.MustCompile(`(?s)---\s*FILE:\s*(\S+)\s*---\n(.*?)---\s*END FILE\s*---`)
	matches := filePattern.FindAllStringSubmatch(resp, -1)
	for _, m := range matches {
		filename := strings.TrimSpace(m[1])
		content := m[2]
		// Only accept changes to target files.
		for _, tf := range targetFiles {
			if filename == tf {
				changes[filename] = content
				break
			}
		}
	}

	return hypothesis, changes
}

// --- Parallel experiments ---

// hypothesisResult holds a parsed hypothesis from a multi-hypothesis LLM response.
type hypothesisResult struct {
	hypothesis string
	changes    map[string]string // filename -> content (file mode)
	overrides  map[string]string // constant name -> value (constants mode)
}

// parseMultiHypothesisResponse parses N hypotheses from a single LLM response.
// Expected format:
//
//	=== HYPOTHESIS 1 ===
//	HYPOTHESIS: ...
//	--- FILE: path ---
//	...
//	--- END FILE ---
//	=== HYPOTHESIS 2 ===
//	...
//
// Falls back to single-hypothesis parsing if no multi markers found.
func parseMultiHypothesisResponse(resp string, n int, targetFiles []string) []hypothesisResult {
	// Split on hypothesis markers.
	marker := regexp.MustCompile(`(?m)^=== HYPOTHESIS \d+ ===\s*$`)
	indices := marker.FindAllStringIndex(resp, -1)

	if len(indices) == 0 {
		// Fallback: parse as single hypothesis.
		hyp, changes := parseLLMResponse(resp, targetFiles)
		if hyp == "" && len(changes) == 0 {
			return nil
		}
		return []hypothesisResult{{hypothesis: hyp, changes: changes}}
	}

	var results []hypothesisResult
	for i, idx := range indices {
		var section string
		if i+1 < len(indices) {
			section = resp[idx[1]:indices[i+1][0]]
		} else {
			section = resp[idx[1]:]
		}
		hyp, changes := parseLLMResponse(section, targetFiles)
		if hyp == "" && len(changes) == 0 {
			continue
		}
		results = append(results, hypothesisResult{hypothesis: hyp, changes: changes})
	}

	// Cap to requested count.
	if len(results) > n {
		results = results[:n]
	}
	return results
}

// parseMultiConstantsResponse parses N constant-override hypotheses from a single LLM response.
func parseMultiConstantsResponse(resp string, n int, constants []ConstantDef) []hypothesisResult {
	marker := regexp.MustCompile(`(?m)^=== HYPOTHESIS \d+ ===\s*$`)
	indices := marker.FindAllStringIndex(resp, -1)

	if len(indices) == 0 {
		hyp, overrides := parseConstantsLLMResponse(resp, constants)
		if hyp == "" && len(overrides) == 0 {
			return nil
		}
		return []hypothesisResult{{hypothesis: hyp, overrides: overrides}}
	}

	var results []hypothesisResult
	for i, idx := range indices {
		var section string
		if i+1 < len(indices) {
			section = resp[idx[1]:indices[i+1][0]]
		} else {
			section = resp[idx[1]:]
		}
		hyp, overrides := parseConstantsLLMResponse(section, constants)
		if hyp == "" && len(overrides) == 0 {
			continue
		}
		results = append(results, hypothesisResult{hypothesis: hyp, overrides: overrides})
	}

	if len(results) > n {
		results = results[:n]
	}
	return results
}
