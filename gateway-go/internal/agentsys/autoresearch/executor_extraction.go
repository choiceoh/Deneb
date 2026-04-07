package autoresearch

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// --- Metric extraction ---

// tailLines returns the last n lines of s.
func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// extractMetricWithPattern extracts a metric using an explicit regex pattern.
// The pattern must have exactly one capture group for the numeric value.
func extractMetricWithPattern(output, pattern string) (float64, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, fmt.Errorf("invalid metric_pattern %q: %w", pattern, err)
	}
	// Search all lines bottom-up for the pattern.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		m := re.FindStringSubmatch(lines[i])
		if len(m) >= 2 {
			val, err := strconv.ParseFloat(strings.TrimSpace(m[1]), 64)
			if err != nil {
				continue
			}
			return val, nil
		}
	}
	return 0, fmt.Errorf("metric_pattern %q matched nothing in output", pattern)
}

// extractMetric finds a floating-point number in the last non-empty line of output.
// This is the default heuristic when no metric_pattern is configured.
func extractMetric(output string) (float64, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		// Try to parse the whole line as a number first.
		if val, err := strconv.ParseFloat(line, 64); err == nil {
			return val, nil
		}
		// Try to find a number in the line (e.g. "val_bpb: 1.087").
		numPattern := regexp.MustCompile(`[-+]?\d+\.?\d*(?:[eE][-+]?\d+)?`)
		matches := numPattern.FindAllString(line, -1)
		if len(matches) > 0 {
			// Take the last number on the line.
			if val, err := strconv.ParseFloat(matches[len(matches)-1], 64); err == nil {
				return val, nil
			}
		}
	}
	return 0, fmt.Errorf("no numeric metric found in output")
}

// extractMetricJSON looks for DENEB_METRIC_JSON {"value": N} in output.
func extractMetricJSON(output string) (float64, bool) {
	const marker = "DENEB_METRIC_JSON "
	idx := strings.LastIndex(output, marker)
	if idx < 0 {
		return 0, false
	}
	jsonStart := idx + len(marker)
	// Find end of JSON object: scan for closing brace.
	depth := 0
	jsonEnd := -1
	for i := jsonStart; i < len(output); i++ {
		switch output[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				jsonEnd = i + 1
			}
		}
		if jsonEnd > 0 {
			break
		}
	}
	if jsonEnd <= 0 {
		return 0, false
	}

	var parsed struct {
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal([]byte(output[jsonStart:jsonEnd]), &parsed); err != nil {
		return 0, false
	}
	return parsed.Value, true
}

// extractMetricKeyValue looks for metric_value=N in output (standard format).
func extractMetricKeyValue(output string) (float64, bool) {
	re := regexp.MustCompile(`metric_value=([-+]?\d+\.?\d*(?:[eE][-+]?\d+)?)`)
	matches := re.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, false
	}
	// Use last match.
	last := matches[len(matches)-1]
	val, err := strconv.ParseFloat(last[1], 64)
	if err != nil {
		return 0, false
	}
	return val, true
}

// extractMetricSmart dispatches to the appropriate extraction method based on
// the configured mode, then validates the result for plausibility (NaN, Inf).
//
// In "auto" mode (default), the priority chain is:
// 1. MetricPattern (explicit regex) — if configured
// 2. DENEB_METRIC_JSON {"value": N} — structured JSON
// 3. metric_value=N — standard key-value format
// 4. Last number heuristic — fallback
func extractMetricSmart(output, pattern string) (float64, error) {
	return extractMetricWithMode(output, pattern, "")
}

// extractMetricWithMode is the full extraction function with mode support.
func extractMetricWithMode(output, pattern, mode string) (float64, error) {
	var val float64
	var err error

	switch mode {
	case "pattern":
		if pattern == "" {
			return 0, fmt.Errorf("metric_extract_mode=pattern but no metric_pattern configured")
		}
		val, err = extractMetricWithPattern(output, pattern)
	case "json":
		v, ok := extractMetricJSON(output)
		if !ok {
			return 0, fmt.Errorf("no DENEB_METRIC_JSON found in output\nOutput tail:\n%s", tailLines(output, 5))
		}
		val = v
	case "last_number":
		val, err = extractMetric(output)
	default: // "auto" or empty
		// Priority chain: pattern > JSON > key-value > heuristic.
		if pattern != "" {
			val, err = extractMetricWithPattern(output, pattern)
		} else if v, ok := extractMetricJSON(output); ok {
			val = v
		} else if v, ok := extractMetricKeyValue(output); ok {
			val = v
		} else {
			val, err = extractMetric(output)
		}
	}

	if err != nil {
		return 0, fmt.Errorf("%w\nOutput tail:\n%s", err, tailLines(output, 5))
	}
	// Validate plausibility.
	if math.IsNaN(val) {
		return 0, fmt.Errorf("metric value is NaN")
	}
	if math.IsInf(val, 0) {
		return 0, fmt.Errorf("metric value is Inf")
	}
	return val, nil
}
