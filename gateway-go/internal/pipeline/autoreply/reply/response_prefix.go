package reply

import (
	"strings"
	"time"
)

// ResponsePrefixTemplate defines the format for response prefix headers.
type ResponsePrefixTemplate struct {
	Format   string // Go template-like format
	Timezone string
}

// FormatResponsePrefix builds a response prefix string from a template.
// Supported placeholders: {timestamp}, {model}, {provider}, {elapsed}.
func FormatResponsePrefix(tmpl ResponsePrefixTemplate, params ResponsePrefixParams) string {
	if tmpl.Format == "" {
		return ""
	}

	result := tmpl.Format

	if strings.Contains(result, "{timestamp}") {
		ts := FormatEnvelopeTimestamp(params.Timestamp, tmpl.Timezone)
		result = strings.ReplaceAll(result, "{timestamp}", ts)
	}
	if strings.Contains(result, "{model}") {
		result = strings.ReplaceAll(result, "{model}", params.Model)
	}
	if strings.Contains(result, "{provider}") {
		result = strings.ReplaceAll(result, "{provider}", params.Provider)
	}
	if strings.Contains(result, "{elapsed}") {
		elapsed := ""
		if params.ElapsedMs > 0 {
			secs := float64(params.ElapsedMs) / 1000.0
			if secs < 1 {
				elapsed = "<1s"
			} else {
				elapsed = strings.TrimRight(strings.TrimRight(
					formatFloat(secs, 1), "0"), ".")
				elapsed += "s"
			}
		}
		result = strings.ReplaceAll(result, "{elapsed}", elapsed)
	}

	return result
}

// ResponsePrefixParams holds the values for response prefix formatting.
type ResponsePrefixParams struct {
	Timestamp time.Time
	Model     string
	Provider  string
	ElapsedMs int64
}

func formatFloat(f float64, prec int) string {
	s := strings.TrimRight(strings.TrimRight(
		strings.Replace(
			strings.Replace(
				strings.Replace(
					formatFloatRaw(f, prec), ".", ".", 1),
				",", "", -1),
			" ", "", -1),
		"0"), ".")
	return s
}

func formatFloatRaw(f float64, prec int) string {
	if prec <= 0 {
		return strings.Split(strings.Replace(
			strings.Replace(
				strings.TrimRight(
					strings.TrimRight(
						formatFloatSimple(f), "0"),
					"."),
				",", "", -1),
			" ", "", -1), ".")[0]
	}
	return formatFloatSimple(f)
}

func formatFloatSimple(f float64) string {
	// Simple float formatting.
	s := ""
	whole := int64(f)
	frac := f - float64(whole)
	s = intToStr(whole)
	if frac > 0.05 {
		s += "."
		digit := int64(frac * 10)
		s += intToStr(digit)
	}
	return s
}

func intToStr(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	if neg {
		return "-" + digits
	}
	return digits
}
