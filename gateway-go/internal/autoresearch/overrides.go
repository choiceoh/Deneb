package autoresearch

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// overridesPath returns the path to overrides.json inside the workspace.
func overridesPath(workdir string) string {
	return filepath.Join(workdir, configDir, "overrides.json")
}

// LoadOverrides reads the best-found overrides from .autoresearch/overrides.json.
func LoadOverrides(workdir string) (*OverrideSet, error) {
	data, err := os.ReadFile(overridesPath(workdir))
	if err != nil {
		return nil, fmt.Errorf("read overrides: %w", err)
	}
	var ov OverrideSet
	if err := json.Unmarshal(data, &ov); err != nil {
		return nil, fmt.Errorf("parse overrides: %w", err)
	}
	if ov.Values == nil {
		ov.Values = make(map[string]string)
	}
	return &ov, nil
}

// SaveOverrides writes the override set to .autoresearch/overrides.json.
func SaveOverrides(workdir string, ov *OverrideSet) error {
	dir := filepath.Join(workdir, configDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create overrides dir: %w", err)
	}
	data, err := json.MarshalIndent(ov, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal overrides: %w", err)
	}
	return os.WriteFile(overridesPath(workdir), data, 0o644)
}

// ExtractConstants reads current values of all defined constants from the
// original source files. Returns name -> current value string.
func ExtractConstants(workdir string, constants []ConstantDef) (map[string]string, error) {
	values := make(map[string]string, len(constants))
	for _, cd := range constants {
		path := filepath.Join(workdir, cd.File)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s for constant %s: %w", cd.File, cd.Name, err)
		}
		pattern := cd.EffectivePattern()
		content := string(data)
		m, err := findWithFallback(content, pattern)
		if err != nil {
			hint := findActualLineHint(content, pattern)
			if hint != "" {
				return nil, fmt.Errorf("constant %s in %s: %w; actual line: %s", cd.Name, cd.File, err, hint)
			}
			return nil, fmt.Errorf("constant %s in %s: %w", cd.Name, cd.File, err)
		}
		values[cd.Name] = m
	}
	return values, nil
}


// ApplyOverrides replaces constant values in target files with the given
// overrides. Returns a restore function that reverts all files to their
// original content. The restore function is safe to call multiple times.
func ApplyOverrides(workdir string, constants []ConstantDef, overrides map[string]string) (restore func(), err error) {
	// Group constants by file to batch replacements.
	type constInfo struct {
		def   ConstantDef
		value string
	}
	byFile := make(map[string][]constInfo)
	for _, cd := range constants {
		val, ok := overrides[cd.Name]
		if !ok {
			continue // not overridden, keep original
		}
		byFile[cd.File] = append(byFile[cd.File], constInfo{def: cd, value: val})
	}

	// Capture originals for restore.
	originals := make(map[string][]byte)
	var once sync.Once

	restoreFn := func() {
		once.Do(func() {
			for file, content := range originals {
				path := filepath.Join(workdir, file)
				_ = os.WriteFile(path, content, 0o644)
			}
		})
	}

	for file, infos := range byFile {
		path := filepath.Join(workdir, file)
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return restoreFn, fmt.Errorf("read %s: %w", file, readErr)
		}
		originals[file] = data
		content := string(data)

		for _, ci := range infos {
			// Validate type and bounds before applying.
			if validErr := validateOverrideValue(ci.def, ci.value); validErr != nil {
				return restoreFn, validErr
			}
			replaced, replErr := replaceCapture(content, ci.def.Pattern, ci.value)
			if replErr != nil {
				return restoreFn, fmt.Errorf("replace %s in %s: %w", ci.def.Name, file, replErr)
			}
			content = replaced
		}

		if writeErr := os.WriteFile(path, []byte(content), 0o644); writeErr != nil {
			restoreFn() // best-effort restore on failure
			return restoreFn, fmt.Errorf("write %s: %w", file, writeErr)
		}
	}

	return restoreFn, nil
}

// replaceCapture replaces the first capture group match of the pattern in
// content with newValue. Everything outside the capture group is preserved.
// If the exact pattern fails, it tries relaxed fallback patterns (flexible
// leading whitespace) so that minor pattern mistakes don't break the run.
func replaceCapture(content, pattern, newValue string) (string, error) {
	loc, err := findSubmatchIndexWithFallback(content, pattern)
	if err != nil {
		return "", err
	}
	// loc[2] and loc[3] are the start/end of capture group 1.
	var sb strings.Builder
	sb.WriteString(content[:loc[2]])
	sb.WriteString(newValue)
	sb.WriteString(content[loc[3]:])
	return sb.String(), nil
}

// findActualLineHint searches content for a line containing the identifier
// extracted from the pattern (strips regex anchors/quantifiers). Helps
// debugging when a pattern fails to match due to indentation or formatting.
func findActualLineHint(content, pattern string) string {
	// Extract the leading identifier from the pattern (e.g. "^myConst\s*=" -> "myConst").
	ident := pattern
	ident = strings.TrimPrefix(ident, "^")
	// Cut at first non-word character (backslash, paren, bracket, etc.).
	for i, ch := range ident {
		if ch == '\\' || ch == '(' || ch == '[' || ch == '{' || ch == '.' || ch == '*' || ch == '+' || ch == '?' || ch == '|' || ch == '$' || ch == ' ' {
			ident = ident[:i]
			break
		}
	}
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return ""
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, ident) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// findWithFallback tries the pattern, then relaxed variants. Returns the
// first capture group match.
func findWithFallback(content, pattern string) (string, error) {
	for _, p := range patternVariants(pattern) {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		m := re.FindStringSubmatch(content)
		if len(m) >= 2 {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("pattern %q (and relaxed variants) matched nothing", pattern)
}

// findSubmatchIndexWithFallback tries the pattern, then relaxed variants.
// Returns the full submatch index slice.
func findSubmatchIndexWithFallback(content, pattern string) ([]int, error) {
	for _, p := range patternVariants(pattern) {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		loc := re.FindStringSubmatchIndex(content)
		if loc != nil && len(loc) >= 4 {
			return loc, nil
		}
	}
	return nil, fmt.Errorf("pattern %q (and relaxed variants) matched nothing", pattern)
}

// patternVariants returns the original pattern plus relaxed alternatives that
// handle common AI-agent mistakes (wrong leading whitespace, literal \t vs
// actual tab, missing word boundary, etc.).
func patternVariants(pattern string) []string {
	variants := []string{pattern}

	// Variant: prepend \s* if the pattern starts with a word character or \b.
	// This handles the case where the agent forgot leading whitespace (e.g.,
	// "weightHybrid\s*=..." when the file has "\tweightHybrid = ...").
	if len(pattern) > 0 {
		first := pattern[0]
		if first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z' || first == '_' {
			variants = append(variants, `\s*`+pattern)
		}
	}

	// Variant: strip explicit leading whitespace anchors (\t, [ \t]+, ^\s*, etc.)
	// and replace with flexible \s*.
	stripped := stripLeadingWSPattern(pattern)
	if stripped != pattern {
		variants = append(variants, `\s*`+stripped)
	}

	return variants
}

// stripLeadingWSPattern removes common leading-whitespace pattern prefixes.
func stripLeadingWSPattern(pattern string) string {
	prefixes := []string{
		`^\s*`, `^\s+`, `^[ \t]*`, `^[ \t]+`, `^\t+`, `^\t`,
		`\s*`, `\s+`, `[ \t]*`, `[ \t]+`, `\t+`, `\t`,
	}
	for _, pfx := range prefixes {
		if strings.HasPrefix(pattern, pfx) {
			rest := pattern[len(pfx):]
			// Only strip if the remainder starts with something meaningful.
			if len(rest) > 0 {
				first := rest[0]
				if first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z' || first == '_' || first == '(' || first == '[' {
					return rest
				}
			}
		}
	}
	return pattern
}

// validateOverrideValue checks that the value matches the constant's type
// and optional bounds.
func validateOverrideValue(cd ConstantDef, value string) error {
	switch cd.Type {
	case "float":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("constant %s: value %q is not a valid float", cd.Name, value)
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("constant %s: value %q is not a finite float", cd.Name, value)
		}
		if cd.Min != nil && v < *cd.Min {
			return fmt.Errorf("constant %s: value %s below min %v", cd.Name, value, *cd.Min)
		}
		if cd.Max != nil && v > *cd.Max {
			return fmt.Errorf("constant %s: value %s above max %v", cd.Name, value, *cd.Max)
		}
	case "int":
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("constant %s: value %q is not a valid int", cd.Name, value)
		}
		if cd.Min != nil && float64(v) < *cd.Min {
			return fmt.Errorf("constant %s: value %s below min %v", cd.Name, value, *cd.Min)
		}
		if cd.Max != nil && float64(v) > *cd.Max {
			return fmt.Errorf("constant %s: value %s above max %v", cd.Name, value, *cd.Max)
		}
	case "string":
		// No validation for string type.
	}
	return nil
}

// parseConstantsLLMResponse extracts hypothesis and override values from
// the LLM response in constants mode. Expected format:
//
//	HYPOTHESIS: <description>
//	LEARNING_RATE = 0.002
//	BATCH_SIZE = 64
func parseConstantsLLMResponse(resp string, constants []ConstantDef) (string, map[string]string) {
	// Build lookup of valid constant names.
	validNames := make(map[string]bool, len(constants))
	for _, cd := range constants {
		validNames[cd.Name] = true
	}

	var hypothesis string
	overrides := make(map[string]string)

	lines := strings.Split(resp, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract hypothesis.
		if strings.HasPrefix(line, "HYPOTHESIS:") {
			hypothesis = strings.TrimSpace(strings.TrimPrefix(line, "HYPOTHESIS:"))
			continue
		}
		// Try NAME = VALUE or NAME: VALUE pattern.
		var name, value string
		if idx := strings.Index(line, "="); idx > 0 {
			name = strings.TrimSpace(line[:idx])
			value = strings.TrimSpace(line[idx+1:])
		} else if idx := strings.Index(line, ":"); idx > 0 && !strings.HasPrefix(line, "HYPOTHESIS:") {
			name = strings.TrimSpace(line[:idx])
			value = strings.TrimSpace(line[idx+1:])
		}
		if name != "" && value != "" && validNames[name] {
			overrides[name] = value
		}
	}

	return hypothesis, overrides
}
