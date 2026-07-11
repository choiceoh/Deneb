// Package common provides dependency-free helpers shared by genesis subsystems.
package common

import (
	"os"
	"strconv"
	"strings"
	"unicode"
)

// TruncateRunes caps text without splitting a Unicode code point.
func TruncateRunes(value string, max int) string {
	if max <= 0 {
		if value == "" {
			return ""
		}
		return "..."
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "..."
}

// ErrorString returns an empty string for nil errors.
func ErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// EnvInt reads an integer environment override with a fallback.
func EnvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// EnvBool reads a conventional boolean environment override with a fallback.
func EnvBool(name string, fallback bool) bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv(name))) {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// SanitizeSkillName normalizes a generated skill name to lowercase hyphens.
func SanitizeSkillName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "_", "-")
	var out strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out.WriteRune(r)
		}
	}
	result := out.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if len(result) < 2 {
		return ""
	}
	return result
}

// SkillDedupTokens builds the normalized token set used by duplicate checks.
func SkillDedupTokens(name, description string) map[string]struct{} {
	set := make(map[string]struct{})
	addTokens(set, name)
	addTokens(set, description)
	return set
}

func addTokens(set map[string]struct{}, text string) {
	for _, token := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(token)) >= 2 {
			set[token] = struct{}{}
		}
	}
}

// JaccardSimilarity returns the intersection-over-union of two token sets.
func JaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for token := range a {
		if _, ok := b[token]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
